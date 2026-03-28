package crashloop_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rohankaran/kubectl-crashloop/internal/crashloop"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestInspectMergesEventsAndLastTerminationState(t *testing.T) {
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
						Reason:     "OOMKilled",
						ExitCode:   137,
						Message:    "Killed after crossing memory limit.",
						FinishedAt: metav1.NewTime(base),
					},
				},
			}},
		},
	}

	sameCrashEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "same-crash",
			Namespace:         "prod",
			CreationTimestamp: metav1.NewTime(base.Add(2 * time.Second)),
		},
		InvolvedObject: corev1.ObjectReference{
			UID:       "pod-uid",
			Namespace: "prod",
			Name:      "api-pod",
			FieldPath: "spec.containers{api}",
		},
		Type:          corev1.EventTypeWarning,
		Reason:        "OOMKilled",
		Message:       "Container api OOMKilled after crossing memory limit",
		LastTimestamp: metav1.NewTime(base.Add(2 * time.Second)),
	}

	olderEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "older-crash",
			Namespace:         "prod",
			CreationTimestamp: metav1.NewTime(base.Add(-3 * time.Minute)),
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
		LastTimestamp: metav1.NewTime(base.Add(-3 * time.Minute)),
	}

	inspector := crashloop.NewInspector(
		fake.NewSimpleClientset(pod, sameCrashEvent, olderEvent),
		crashloop.WithNowFunc(func() time.Time { return base.Add(10 * time.Minute) }),
		crashloop.WithLogFetcher(func(context.Context, string, string, string, int64, bool) (string, error) {
			return "panic: runtime error: out of memory", nil
		}),
	)

	report, err := inspector.Inspect(context.Background(), crashloop.Request{
		Namespace:   "prod",
		ContextName: "kind-prod",
		PodName:     "api-pod",
		TailLines:   5,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if len(report.Entries) != 2 {
		t.Fatalf("len(report.Entries) = %d, want 2", len(report.Entries))
	}
	if report.Entries[0].Source != crashloop.SourceLastTerminationState {
		t.Fatalf("first source = %s, want %s", report.Entries[0].Source, crashloop.SourceLastTerminationState)
	}
	if report.Entries[0].ExitCode == nil || *report.Entries[0].ExitCode != 137 {
		t.Fatalf("first exit code = %v, want 137", report.Entries[0].ExitCode)
	}
	if !strings.Contains(report.Entries[0].TailLogs, "out of memory") {
		t.Fatalf("expected merged entry to contain previous logs, got %q", report.Entries[0].TailLogs)
	}
	if report.Entries[0].TailLogSource != crashloop.TailLogSourcePrevious {
		t.Fatalf("first log source = %s, want %s", report.Entries[0].TailLogSource, crashloop.TailLogSourcePrevious)
	}
	if report.Entries[1].Source != crashloop.SourceEvent {
		t.Fatalf("second source = %s, want %s", report.Entries[1].Source, crashloop.SourceEvent)
	}
}

func TestInspectAddsBaselineWarningWhenEventsExpire(t *testing.T) {
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
			return "", nil
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
	if len(report.Warnings) == 0 || !strings.Contains(report.Warnings[0], "Historical Events may have expired") {
		t.Fatalf("expected TTL warning, got %#v", report.Warnings)
	}
}

func TestInspectSupportsMultiContainerSortingAndFilter(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-pod",
			Namespace: "prod",
			UID:       "pod-uid",
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "api",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:     "Error",
							ExitCode:   1,
							FinishedAt: metav1.NewTime(base.Add(-1 * time.Minute)),
						},
					},
				},
				{
					Name: "worker",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:     "OOMKilled",
							ExitCode:   137,
							FinishedAt: metav1.NewTime(base.Add(-2 * time.Minute)),
						},
					},
				},
			},
		},
	}

	workerEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "worker-backoff",
			Namespace:         "prod",
			CreationTimestamp: metav1.NewTime(base.Add(-30 * time.Second)),
		},
		InvolvedObject: corev1.ObjectReference{
			UID:       "pod-uid",
			Namespace: "prod",
			Name:      "api-pod",
			FieldPath: "spec.containers{worker}",
		},
		Type:          corev1.EventTypeWarning,
		Reason:        "BackOff",
		Message:       "Back-off restarting failed container worker in pod api-pod_prod",
		LastTimestamp: metav1.NewTime(base.Add(-30 * time.Second)),
	}

	inspector := crashloop.NewInspector(
		fake.NewSimpleClientset(pod, workerEvent),
		crashloop.WithLogFetcher(func(context.Context, string, string, string, int64, bool) (string, error) {
			return "", nil
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

	if len(report.Entries) != 3 {
		t.Fatalf("len(report.Entries) = %d, want 3", len(report.Entries))
	}
	if report.Entries[0].Container != "worker" {
		t.Fatalf("first container = %q, want worker", report.Entries[0].Container)
	}
	if report.Entries[1].Container != "api" {
		t.Fatalf("second container = %q, want api", report.Entries[1].Container)
	}

	filtered, err := inspector.Inspect(context.Background(), crashloop.Request{
		Namespace: "prod",
		PodName:   "api-pod",
		Container: "worker",
		TailLines: 5,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Inspect() with filter error = %v", err)
	}

	for _, entry := range filtered.Entries {
		if entry.Container != "worker" {
			t.Fatalf("filtered entry container = %q, want worker", entry.Container)
		}
	}
}

func TestInspectDoesNotMergeDistinctGenericErrorCrashes(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 22, 6, 1, 3, 0, time.UTC)
	exit1 := 1
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
						ExitCode:   int32(exit1),
						Message:    "process exited unexpectedly",
						FinishedAt: metav1.NewTime(base),
					},
				},
			}},
		},
	}

	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "api-error",
			Namespace:         "prod",
			CreationTimestamp: metav1.NewTime(base.Add(2 * time.Second)),
		},
		InvolvedObject: corev1.ObjectReference{
			UID:       "pod-uid",
			Namespace: "prod",
			Name:      "api-pod",
			FieldPath: "spec.containers{api}",
		},
		Type:          corev1.EventTypeWarning,
		Reason:        "Error",
		Message:       "Container api failed during startup probe handling",
		LastTimestamp: metav1.NewTime(base.Add(2 * time.Second)),
	}

	inspector := crashloop.NewInspector(
		fake.NewSimpleClientset(pod, event),
		crashloop.WithLogFetcher(func(context.Context, string, string, string, int64, bool) (string, error) {
			return "stderr output", nil
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

	if len(report.Entries) != 2 {
		t.Fatalf("len(report.Entries) = %d, want 2 distinct entries", len(report.Entries))
	}
	if report.Entries[0].Source != crashloop.SourceEvent {
		t.Fatalf("first source = %q, want %q", report.Entries[0].Source, crashloop.SourceEvent)
	}
	if report.Entries[1].Source != crashloop.SourceLastTerminationState {
		t.Fatalf("second source = %q, want %q", report.Entries[1].Source, crashloop.SourceLastTerminationState)
	}
}

func TestInspectReturnsNamespaceNotFoundWhenPodNotFoundAndNamespaceIsMissing(t *testing.T) {
	t.Parallel()

	inspector := crashloop.NewInspector(
		fake.NewSimpleClientset(),
		crashloop.WithLogFetcher(func(context.Context, string, string, string, int64, bool) (string, error) {
			return "", nil
		}),
	)

	_, err := inspector.Inspect(context.Background(), crashloop.Request{
		Namespace: "missing-ns",
		PodName:   "api-pod",
		TailLines: 5,
		Limit:     10,
	})
	if err == nil {
		t.Fatalf("Inspect() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "namespaces \"missing-ns\" not found") {
		t.Fatalf("expected namespace not found error, got %v", err)
	}
}
