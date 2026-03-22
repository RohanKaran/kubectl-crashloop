package crashloop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "api",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:     "OOMKilled",
							ExitCode:   137,
							Message:    "Killed after crossing memory limit.",
							FinishedAt: metav1.NewTime(base),
						},
					},
				},
			},
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

	client := fake.NewSimpleClientset(pod, sameCrashEvent, olderEvent)
	inspector := NewInspector(client)
	inspector.now = func() time.Time { return base.Add(10 * time.Minute) }
	inspector.logFetcher = func(context.Context, string, string, string, int64, bool) (string, error) {
		return "panic: runtime error: out of memory", nil
	}

	report, err := inspector.Inspect(context.Background(), Request{
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
	if report.Entries[0].Source != SourceLastTerminationState {
		t.Fatalf("first source = %s, want %s", report.Entries[0].Source, SourceLastTerminationState)
	}
	if report.Entries[0].ExitCode == nil || *report.Entries[0].ExitCode != 137 {
		t.Fatalf("first exit code = %v, want 137", report.Entries[0].ExitCode)
	}
	if !strings.Contains(report.Entries[0].TailLogs, "out of memory") {
		t.Fatalf("expected merged entry to contain previous logs, got %q", report.Entries[0].TailLogs)
	}
	if report.Entries[0].TailLogSource != TailLogSourcePrevious {
		t.Fatalf("first log source = %s, want %s", report.Entries[0].TailLogSource, TailLogSourcePrevious)
	}
	if report.Entries[1].Source != SourceEvent {
		t.Fatalf("second source = %s, want %s", report.Entries[1].Source, SourceEvent)
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
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "api",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:     "Error",
							ExitCode:   1,
							FinishedAt: metav1.NewTime(base),
						},
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(pod)
	inspector := NewInspector(client)
	inspector.logFetcher = func(context.Context, string, string, string, int64, bool) (string, error) {
		return "", nil
	}

	report, err := inspector.Inspect(context.Background(), Request{
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

	client := fake.NewSimpleClientset(pod, workerEvent)
	inspector := NewInspector(client)
	inspector.logFetcher = func(context.Context, string, string, string, int64, bool) (string, error) {
		return "", nil
	}

	report, err := inspector.Inspect(context.Background(), Request{
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

	filtered, err := inspector.Inspect(context.Background(), Request{
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

func TestInspectKeepsPodWideEventsUnattributed(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 22, 6, 1, 3, 0, time.UTC)
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
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"},
					},
				},
			},
		},
	}

	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "failed-mount",
			Namespace:         "prod",
			CreationTimestamp: metav1.NewTime(base),
		},
		InvolvedObject: corev1.ObjectReference{
			UID:       "pod-uid",
			Namespace: "prod",
			Name:      "api-pod",
		},
		Type:          corev1.EventTypeWarning,
		Reason:        "FailedMount",
		Message:       "Unable to attach or mount volumes: unmounted volumes=[data]",
		LastTimestamp: metav1.NewTime(base),
	}

	client := fake.NewSimpleClientset(pod, event)
	inspector := NewInspector(client)
	inspector.logFetcher = func(context.Context, string, string, string, int64, bool) (string, error) {
		t.Fatal("log fetcher should not be called for unattributed pod-wide events")
		return "", nil
	}

	report, err := inspector.Inspect(context.Background(), Request{
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
	if report.Entries[0].Container != "" {
		t.Fatalf("entry container = %q, want pod-wide event", report.Entries[0].Container)
	}
	if report.Entries[0].TailLogs != "" {
		t.Fatalf("tail logs = %q, want none for pod-wide event", report.Entries[0].TailLogs)
	}
}

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
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "api",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:     "Error",
							ExitCode:   1,
							FinishedAt: metav1.NewTime(base),
						},
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(pod)
	inspector := NewInspector(client)
	inspector.logFetcher = func(context.Context, string, string, string, int64, bool) (string, error) {
		return "", errors.New("pods/log is forbidden")
	}

	report, err := inspector.Inspect(context.Background(), Request{
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
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "api",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
					RestartCount: 9,
				},
			},
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

	client := fake.NewSimpleClientset(pod, event)
	inspector := NewInspector(client)
	inspector.logFetcher = func(_ context.Context, _, _, _ string, _ int64, previous bool) (string, error) {
		if previous {
			t.Fatal("previous logs should not be requested when last termination state is missing")
		}
		return "booting worker\nfailing on purpose", nil
	}

	report, err := inspector.Inspect(context.Background(), Request{
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
	if report.Entries[0].Source != SourceEvent {
		t.Fatalf("entry source = %q, want %q", report.Entries[0].Source, SourceEvent)
	}
	if report.Entries[0].TailLogs != "booting worker\nfailing on purpose" {
		t.Fatalf("tail logs = %q, want current logs fallback", report.Entries[0].TailLogs)
	}
	if report.Entries[0].TailLogSource != TailLogSourceCurrent {
		t.Fatalf("tail log source = %q, want %q", report.Entries[0].TailLogSource, TailLogSourceCurrent)
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
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "api",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:     "Error",
							ExitCode:   42,
							FinishedAt: metav1.NewTime(base),
						},
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(pod)
	inspector := NewInspector(client)
	inspector.logFetcher = func(_ context.Context, _, _, _ string, _ int64, previous bool) (string, error) {
		if previous {
			return "unable to retrieve container logs for containerd://deadbeef", nil
		}
		return "starting\nfailing on purpose", nil
	}

	report, err := inspector.Inspect(context.Background(), Request{
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
	if report.Entries[0].TailLogSource != TailLogSourceCurrent {
		t.Fatalf("tail log source = %q, want %q", report.Entries[0].TailLogSource, TailLogSourceCurrent)
	}
	if len(report.Warnings) == 0 || !strings.Contains(report.Warnings[0], "Showing current container logs instead.") {
		t.Fatalf("expected fallback warning, got %#v", report.Warnings)
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
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "api",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:     "Error",
							ExitCode:   int32(exit1),
							Message:    "process exited unexpectedly",
							FinishedAt: metav1.NewTime(base),
						},
					},
				},
			},
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

	client := fake.NewSimpleClientset(pod, event)
	inspector := NewInspector(client)
	inspector.logFetcher = func(context.Context, string, string, string, int64, bool) (string, error) {
		return "stderr output", nil
	}

	report, err := inspector.Inspect(context.Background(), Request{
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
	if report.Entries[0].Source != SourceEvent {
		t.Fatalf("first source = %q, want %q", report.Entries[0].Source, SourceEvent)
	}
	if report.Entries[1].Source != SourceLastTerminationState {
		t.Fatalf("second source = %q, want %q", report.Entries[1].Source, SourceLastTerminationState)
	}
}
