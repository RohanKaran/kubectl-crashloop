package crashloop_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rohankaran/kubectl-crashloop/internal/crashloop"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

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
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "api",
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"},
				},
			}},
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

	inspector := crashloop.NewInspector(
		fake.NewSimpleClientset(pod, event),
		crashloop.WithLogFetcher(func(context.Context, string, string, string, int64, bool) (string, error) {
			t.Fatal("log fetcher should not be called for unattributed pod-wide events")
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
	if report.Entries[0].Container != "" {
		t.Fatalf("entry container = %q, want pod-wide event", report.Entries[0].Container)
	}
	if report.Entries[0].TailLogs != "" {
		t.Fatalf("tail logs = %q, want none for pod-wide event", report.Entries[0].TailLogs)
	}
}

func TestInspectParsesPodEvictions(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 22, 6, 1, 3, 0, time.UTC)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "api-pod",
			Namespace:         "prod",
			UID:               "pod-uid",
			CreationTimestamp: metav1.NewTime(base),
		},
		Status: corev1.PodStatus{
			Phase:   corev1.PodFailed,
			Reason:  "Evicted",
			Message: "The node was low on resource: memory. Threshold quantity: 100Mi, available: 50Mi.",
			Conditions: []corev1.PodCondition{
				{
					Type:               corev1.PodReady,
					Status:             corev1.ConditionFalse,
					LastTransitionTime: metav1.NewTime(base.Add(10 * time.Minute)),
				},
			},
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
	if report.Entries[0].Source != crashloop.SourcePodStatus {
		t.Fatalf("source = %q, want %q", report.Entries[0].Source, crashloop.SourcePodStatus)
	}
	if report.Entries[0].Timestamp.UTC() != base.Add(10*time.Minute).UTC() {
		t.Fatalf("timestamp = %v, want %v", report.Entries[0].Timestamp, base.Add(10*time.Minute))
	}
	if !strings.Contains(report.Entries[0].Message, "memory") {
		t.Fatalf("expected eviction message, got %q", report.Entries[0].Message)
	}
}

func TestInspectCorrelatesNodeSystemOOM(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 22, 6, 1, 3, 0, time.UTC)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-pod",
			Namespace: "prod",
			UID:       "pod-uid",
		},
		Spec: corev1.PodSpec{
			NodeName: "worker-node-1",
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "api",
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						Reason:     "OOMKilled",
						ExitCode:   137,
						FinishedAt: metav1.NewTime(base),
					},
				},
			}},
		},
	}

	nodeEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "node-oom",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(base.Add(2 * time.Second)),
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Node",
			Name: "worker-node-1",
		},
		Type:          corev1.EventTypeWarning,
		Reason:        "SystemOOM",
		Message:       "System OOM encountered, victim process: my-app",
		LastTimestamp: metav1.NewTime(base.Add(2 * time.Second)),
	}

	inspector := crashloop.NewInspector(
		fake.NewSimpleClientset(pod, nodeEvent),
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

	if len(report.Entries) != 2 {
		t.Fatalf("len(report.Entries) = %d, want 2 (Container OOM and Node OOM)", len(report.Entries))
	}

	var hasNodeEvent bool
	for _, entry := range report.Entries {
		if entry.Source == crashloop.SourceNodeEvent {
			hasNodeEvent = true
			if entry.Reason != "SystemOOM" {
				t.Fatalf("expected Reason SystemOOM, got %q", entry.Reason)
			}
			if !strings.Contains(entry.Message, "worker-node-1") {
				t.Fatalf("expected node name in message, got %q", entry.Message)
			}
		}
	}

	if !hasNodeEvent {
		t.Fatal("expected report to contain a SourceNodeEvent entry")
	}
}

func TestInspectIgnoresStaleNodeSystemOOM(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 22, 6, 1, 3, 0, time.UTC)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "api-pod",
			Namespace:         "prod",
			UID:               "pod-uid",
			CreationTimestamp: metav1.NewTime(base.Add(-20 * time.Minute)),
		},
		Spec: corev1.PodSpec{
			NodeName: "worker-node-1",
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "api",
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						Reason:     "OOMKilled",
						ExitCode:   137,
						FinishedAt: metav1.NewTime(base),
					},
				},
			}},
		},
	}

	nodeEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "node-oom",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(base.Add(-10 * time.Minute)),
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Node",
			Name: "worker-node-1",
		},
		Type:          corev1.EventTypeWarning,
		Reason:        "SystemOOM",
		Message:       "System OOM encountered long before the current crash",
		LastTimestamp: metav1.NewTime(base.Add(-10 * time.Minute)),
	}

	inspector := crashloop.NewInspector(
		fake.NewSimpleClientset(pod, nodeEvent),
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

	for _, entry := range report.Entries {
		if entry.Source == crashloop.SourceNodeEvent {
			t.Fatalf("expected stale node event to be ignored, got %#v", entry)
		}
	}
}

func TestInspectKeepsEvictedStatusWhenEventListingIsForbidden(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 22, 6, 1, 3, 0, time.UTC)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "api-pod",
			Namespace:         "prod",
			UID:               "pod-uid",
			CreationTimestamp: metav1.NewTime(base),
		},
		Status: corev1.PodStatus{
			Phase:   corev1.PodFailed,
			Reason:  "Evicted",
			Message: "The node was low on resource: memory. Threshold quantity: 100Mi, available: 50Mi.",
			Conditions: []corev1.PodCondition{
				{
					Type:               corev1.PodReady,
					Status:             corev1.ConditionFalse,
					LastTransitionTime: metav1.NewTime(base.Add(10 * time.Minute)),
				},
			},
		},
	}

	client := fake.NewSimpleClientset(pod)
	client.PrependReactor("list", "events", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "events"}, "", errors.New("events are forbidden"))
	})

	inspector := crashloop.NewInspector(
		client,
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
	if report.Entries[0].Source != crashloop.SourcePodStatus {
		t.Fatalf("source = %q, want %q", report.Entries[0].Source, crashloop.SourcePodStatus)
	}
	if len(report.Warnings) == 0 {
		t.Fatal("expected warning about forbidden event listing")
	}
}

func TestInspectFallsBackWhenNodeEventFieldSelectorsAreUnsupported(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 22, 6, 1, 3, 0, time.UTC)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-pod",
			Namespace: "prod",
			UID:       "pod-uid",
		},
		Spec: corev1.PodSpec{
			NodeName: "worker-node-1",
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "api",
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						Reason:     "OOMKilled",
						ExitCode:   137,
						FinishedAt: metav1.NewTime(base),
					},
				},
			}},
		},
	}

	nodeEvent := corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "node-oom",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(base.Add(2 * time.Second)),
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Node",
			Name: "worker-node-1",
		},
		Type:          corev1.EventTypeWarning,
		Reason:        "SystemOOM",
		Message:       "System OOM encountered, victim process: my-app",
		LastTimestamp: metav1.NewTime(base.Add(2 * time.Second)),
	}

	client := fake.NewSimpleClientset(pod)
	client.PrependReactor("list", "events", func(action k8stesting.Action) (bool, runtime.Object, error) {
		listAction := action.(k8stesting.ListAction)
		selector := listAction.GetListRestrictions().Fields.String()

		switch {
		case strings.Contains(selector, "involvedObject.uid="):
			return true, &corev1.EventList{}, nil
		case strings.Contains(selector, "involvedObject.kind=Node"):
			return true, nil, apierrors.NewBadRequest("field selector not supported")
		case selector == "type=Warning":
			return true, &corev1.EventList{Items: []corev1.Event{nodeEvent}}, nil
		default:
			return true, &corev1.EventList{}, nil
		}
	})

	inspector := crashloop.NewInspector(
		client,
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

	for _, entry := range report.Entries {
		if entry.Source == crashloop.SourceNodeEvent {
			return
		}
	}

	t.Fatalf("expected report to include a node event after fallback, got %#v", report.Entries)
}
