package crashloop_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rohankaran/kubectl-crashloop/internal/crashloop"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestInspectWarnsWhenPreviousLogsAreUnavailable(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-pod",
			Namespace: "prod",
			UID:       "pod-uid",
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "api",
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						Reason:     "Error",
						ExitCode:   1,
						FinishedAt: metav1.NewTime(base),
					},
				},
			}},
		},
	}

	inspector := crashloop.NewInspector(
		fake.NewSimpleClientset(pod),
		crashloop.WithLogFetcher(func(context.Context, string, string, string, int64, bool) (string, error) {
			return "", errors.New("pods/log is forbidden")
		}),
	)

	report, err := inspector.Inspect(context.Background(), crashloop.Request{
		Namespace: "prod",
		PodName:   "api-pod",
		TailLines: 5,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if len(report.Entries) != 1 {
		t.Fatalf("len(report.Entries) = %d, want 1", len(report.Entries))
	}
	if len(report.Warnings) == 0 || !strings.Contains(strings.ToLower(report.Warnings[0]), "previous logs") {
		t.Fatalf("expected previous logs warning, got %#v", report.Warnings)
	}
	if report.Entries[0].TailLogs != "" {
		t.Fatalf("expected empty tail logs, got %q", report.Entries[0].TailLogs)
	}
	if report.Entries[0].TailLogSource != "" {
		t.Fatalf("expected empty tail log source, got %q", report.Entries[0].TailLogSource)
	}
}

func TestInspectAttachesCurrentLogsToLatestEventWhenLastTerminationStateIsMissing(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 22, 6, 1, 3, 0, time.UTC)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-pod",
			Namespace: "prod",
			UID:       "pod-uid",
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "api",
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
				},
				RestartCount: 9,
			}},
		},
	}

	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "api-backoff",
			Namespace:         "prod",
			CreationTimestamp: metav1.NewTime(base),
		},
		InvolvedObject: corev1.ObjectReference{
			UID:       "pod-uid",
			Namespace: "prod",
			Name:      "api-pod",
			FieldPath: "spec.containers{api}",
		},
		Type:          corev1.EventTypeWarning,
		Reason:        "BackOff",
		Message:       "Back-off restarting failed container api in pod api-pod_prod",
		LastTimestamp: metav1.NewTime(base),
	}

	inspector := crashloop.NewInspector(
		fake.NewSimpleClientset(pod, event),
		crashloop.WithLogFetcher(func(_ context.Context, _, _, _ string, _ int64, previous bool) (string, error) {
			if previous {
				t.Fatal("previous logs should not be requested when last termination state is missing")
			}
			return "booting worker\nfailing on purpose", nil
		}),
	)

	report, err := inspector.Inspect(context.Background(), crashloop.Request{
		Namespace: "prod",
		PodName:   "api-pod",
		TailLines: 5,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if len(report.Entries) != 1 {
		t.Fatalf("len(report.Entries) = %d, want 1", len(report.Entries))
	}
	if report.Entries[0].Source != crashloop.SourceEvent {
		t.Fatalf("entry source = %q, want %q", report.Entries[0].Source, crashloop.SourceEvent)
	}
	if report.Entries[0].TailLogs != "booting worker\nfailing on purpose" {
		t.Fatalf("tail logs = %q, want current logs fallback", report.Entries[0].TailLogs)
	}
	if report.Entries[0].TailLogSource != crashloop.TailLogSourceCurrent {
		t.Fatalf("tail log source = %q, want %q", report.Entries[0].TailLogSource, crashloop.TailLogSourceCurrent)
	}
	if len(report.Warnings) == 0 || !strings.Contains(report.Warnings[0], "latest warning event") {
		t.Fatalf("expected latest warning event warning, got %#v", report.Warnings)
	}
}

func TestInspectFallsBackToCurrentLogsWhenPreviousLogsPayloadIsUnavailable(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-pod",
			Namespace: "prod",
			UID:       "pod-uid",
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "api",
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						Reason:     "Error",
						ExitCode:   42,
						FinishedAt: metav1.NewTime(base),
					},
				},
			}},
		},
	}

	inspector := crashloop.NewInspector(
		fake.NewSimpleClientset(pod),
		crashloop.WithLogFetcher(func(_ context.Context, _, _, _ string, _ int64, previous bool) (string, error) {
			if previous {
				return "unable to retrieve container logs for containerd://deadbeef", nil
			}
			return "starting\nfailing on purpose", nil
		}),
	)

	report, err := inspector.Inspect(context.Background(), crashloop.Request{
		Namespace: "prod",
		PodName:   "api-pod",
		TailLines: 5,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if len(report.Entries) != 1 {
		t.Fatalf("len(report.Entries) = %d, want 1", len(report.Entries))
	}
	if report.Entries[0].TailLogs != "starting\nfailing on purpose" {
		t.Fatalf("tail logs = %q, want fallback current logs", report.Entries[0].TailLogs)
	}
	if report.Entries[0].TailLogSource != crashloop.TailLogSourceCurrent {
		t.Fatalf("tail log source = %q, want %q", report.Entries[0].TailLogSource, crashloop.TailLogSourceCurrent)
	}
	if len(report.Warnings) == 0 || !strings.Contains(report.Warnings[0], "Showing current container logs instead.") {
		t.Fatalf("expected fallback warning, got %#v", report.Warnings)
	}
}
