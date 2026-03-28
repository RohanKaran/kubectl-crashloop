package crashloop

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

var fieldPathContainerPattern = regexp.MustCompile(`\{([^}]+)\}`)

const nodeEventCorrelationWindow = 5 * time.Minute

func (i *Inspector) buildEventEntries(ctx context.Context, pod *corev1.Pod, statuses []containerStatusRef, selectedContainer string) ([]CrashEntry, []string, error) {
	containerNames := namesForStatuses(statuses)
	relevantStatuses := filterStatusesForSelection(statuses, selectedContainer)
	entries := buildPodStatusEntries(pod)
	referenceTimes := relevantNodeReferenceTimes(pod, relevantStatuses)

	items, warnings, err := i.listWarningEvents(ctx, pod.Namespace, string(pod.UID))
	if err != nil {
		return entries, warnings, err
	}

	for _, event := range items {
		if event.InvolvedObject.UID != pod.UID || event.Type != corev1.EventTypeWarning {
			continue
		}

		timestamp, ok := eventTimestamp(event)
		if !ok {
			continue
		}

		containerName := inferContainerName(event, containerNames)
		if selectedContainer != "" && containerName != selectedContainer {
			continue
		}

		if isMemoryRelatedEvent(event.Reason, event.Message) {
			referenceTimes = append(referenceTimes, timestamp.UTC())
		}

		message := strings.TrimSpace(event.Message)
		if event.Count > 1 && message != "" {
			message = fmt.Sprintf("%s (x%d)", message, event.Count)
		}

		entries = append(entries, CrashEntry{
			Container: containerName,
			Timestamp: timestamp.UTC(),
			Reason:    firstNonEmpty(event.Reason, "Warning"),
			Message:   message,
			Source:    SourceEvent,
		})
	}

	nodeEvents, nodeWarnings := i.listNodeWarningEvents(ctx, pod.Spec.NodeName)
	warnings = append(warnings, nodeWarnings...)
	for _, event := range nodeEvents {
		if !isNodeMemoryWarningReason(event.Reason) {
			continue
		}

		timestamp, ok := eventTimestamp(event)
		if !ok || !nodeEventAppliesToPod(timestamp, pod, referenceTimes) {
			continue
		}

		message := strings.TrimSpace(event.Message)
		if event.Count > 1 && message != "" {
			message = fmt.Sprintf("%s (x%d)", message, event.Count)
		}

		entries = append(entries, CrashEntry{
			Timestamp: timestamp.UTC(),
			Reason:    firstNonEmpty(event.Reason, "NodeMemoryPressure"),
			Message:   fmt.Sprintf("Node %s: %s", pod.Spec.NodeName, message),
			Source:    SourceNodeEvent,
		})
	}

	return entries, warnings, nil
}

func (i *Inspector) listWarningEvents(ctx context.Context, namespace, podUID string) ([]corev1.Event, []string, error) {
	selector := fields.AndSelectors(
		fields.OneTermEqualSelector("type", corev1.EventTypeWarning),
		fields.OneTermEqualSelector("involvedObject.uid", podUID),
	)

	list, err := i.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{FieldSelector: selector.String()})
	if err == nil {
		return list.Items, nil, nil
	}

	if !apierrors.IsBadRequest(err) {
		return nil, nil, err
	}

	fallbackSelector := fields.OneTermEqualSelector("type", corev1.EventTypeWarning)
	list, fallbackErr := i.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{FieldSelector: fallbackSelector.String()})
	if fallbackErr != nil {
		return nil, nil, fallbackErr
	}

	return list.Items, []string{"Event field selectors are unavailable on this cluster; falling back to a namespace-wide warning Event scan."}, nil
}

func (i *Inspector) listNodeWarningEvents(ctx context.Context, nodeName string) ([]corev1.Event, []string) {
	if nodeName == "" {
		return nil, nil
	}

	selector := fields.AndSelectors(
		fields.OneTermEqualSelector("type", corev1.EventTypeWarning),
		fields.OneTermEqualSelector("involvedObject.kind", "Node"),
		fields.OneTermEqualSelector("involvedObject.name", nodeName),
	)

	list, err := i.client.CoreV1().Events("").List(ctx, metav1.ListOptions{FieldSelector: selector.String()})
	if err == nil {
		return filterNodeWarningEvents(list.Items, nodeName), nil
	}

	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		list, err = i.client.CoreV1().Events("default").List(ctx, metav1.ListOptions{FieldSelector: selector.String()})
		if err == nil {
			return filterNodeWarningEvents(list.Items, nodeName), nil
		}
	}

	if !apierrors.IsBadRequest(err) {
		return nil, nil
	}

	fallbackSelector := fields.OneTermEqualSelector("type", corev1.EventTypeWarning)
	warning := "Node Event field selectors are unavailable on this cluster; falling back to a cluster-wide warning Event scan."

	list, err = i.client.CoreV1().Events("").List(ctx, metav1.ListOptions{FieldSelector: fallbackSelector.String()})
	if err == nil {
		return filterNodeWarningEvents(list.Items, nodeName), []string{warning}
	}

	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		list, err = i.client.CoreV1().Events("default").List(ctx, metav1.ListOptions{FieldSelector: fallbackSelector.String()})
		if err == nil {
			return filterNodeWarningEvents(list.Items, nodeName), []string{warning}
		}
	}

	return nil, []string{warning}
}

func buildPodStatusEntries(pod *corev1.Pod) []CrashEntry {
	if pod.Status.Reason != "Evicted" {
		return nil
	}

	return []CrashEntry{{
		Timestamp: podStatusTimestamp(pod).UTC(),
		Reason:    "Evicted",
		Message:   firstNonEmpty(pod.Status.Message, "The pod was evicted from the node (likely due to memory or disk pressure)."),
		Source:    SourcePodStatus,
	}}
}

func podStatusTimestamp(pod *corev1.Pod) time.Time {
	timestamp := pod.CreationTimestamp.Time
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionFalse {
			timestamp = cond.LastTransitionTime.Time
		}
	}
	return timestamp
}

func eventTimestamp(event corev1.Event) (time.Time, bool) {
	switch {
	case !event.EventTime.IsZero():
		return event.EventTime.Time, true
	case !event.LastTimestamp.IsZero():
		return event.LastTimestamp.Time, true
	case !event.CreationTimestamp.IsZero():
		return event.CreationTimestamp.Time, true
	default:
		return time.Time{}, false
	}
}

func inferContainerName(event corev1.Event, containerNames []string) string {
	if match := fieldPathContainerPattern.FindStringSubmatch(event.InvolvedObject.FieldPath); len(match) == 2 {
		return match[1]
	}

	message := strings.ToLower(event.Message)
	for _, name := range containerNames {
		lowerName := strings.ToLower(name)
		if strings.Contains(message, "{"+lowerName+"}") ||
			strings.Contains(message, "container "+lowerName) ||
			strings.Contains(message, " "+lowerName+" ") {
			return name
		}
	}

	return ""
}

func relevantNodeReferenceTimes(pod *corev1.Pod, statuses []containerStatusRef) []time.Time {
	referenceTimes := make([]time.Time, 0, len(statuses)+1)

	if pod.Status.Reason == "Evicted" {
		referenceTimes = append(referenceTimes, podStatusTimestamp(pod).UTC())
	}

	for _, status := range statuses {
		terminated := status.Status.LastTerminationState.Terminated
		if terminated == nil || !isMemoryRelatedTermination(terminated) {
			continue
		}

		referenceTimes = append(referenceTimes, terminatedTimestamp(terminated).UTC())
	}

	return referenceTimes
}

func isMemoryRelatedTermination(terminated *corev1.ContainerStateTerminated) bool {
	reason := strings.ToLower(strings.TrimSpace(terminated.Reason))
	message := strings.ToLower(strings.TrimSpace(terminated.Message))

	return reason == "oomkilled" ||
		terminated.ExitCode == 137 ||
		strings.Contains(message, "out of memory") ||
		strings.Contains(message, "oom")
}

func isMemoryRelatedEvent(reason, message string) bool {
	lowerReason := strings.ToLower(strings.TrimSpace(reason))
	lowerMessage := strings.ToLower(strings.TrimSpace(message))

	return strings.Contains(lowerReason, "oom") ||
		strings.Contains(lowerReason, "memorypressure") ||
		strings.Contains(lowerMessage, "out of memory") ||
		strings.Contains(lowerMessage, "memory pressure")
}

func isNodeMemoryWarningReason(reason string) bool {
	normalized := strings.ToLower(strings.TrimSpace(reason))
	return normalized == "systemoom" ||
		normalized == "oomkilling" ||
		strings.Contains(normalized, "memorypressure")
}

func nodeEventAppliesToPod(timestamp time.Time, pod *corev1.Pod, referenceTimes []time.Time) bool {
	if len(referenceTimes) == 0 {
		return false
	}

	if !pod.CreationTimestamp.IsZero() && timestamp.Before(pod.CreationTimestamp.Add(-1*time.Minute)) {
		return false
	}

	for _, reference := range referenceTimes {
		delta := timestamp.Sub(reference)
		if delta < 0 {
			delta = -delta
		}
		if delta <= nodeEventCorrelationWindow {
			return true
		}
	}

	return false
}

func filterNodeWarningEvents(events []corev1.Event, nodeName string) []corev1.Event {
	filtered := make([]corev1.Event, 0, len(events))
	for _, event := range events {
		if event.Type != corev1.EventTypeWarning {
			continue
		}
		if event.InvolvedObject.Kind != "Node" || event.InvolvedObject.Name != nodeName {
			continue
		}
		filtered = append(filtered, event)
	}

	return filtered
}
