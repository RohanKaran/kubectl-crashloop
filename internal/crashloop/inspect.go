package crashloop

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

var fieldPathContainerPattern = regexp.MustCompile(`\{([^}]+)\}`)

type logFetcher func(context.Context, string, string, string, int64, bool) (string, error)

type Inspector struct {
	client     kubernetes.Interface
	logFetcher logFetcher
	now        func() time.Time
}

type containerStatusRef struct {
	Name   string
	Status corev1.ContainerStatus
}

func NewInspector(client kubernetes.Interface) *Inspector {
	return &Inspector{
		client:     client,
		logFetcher: defaultLogFetcher(client),
		now:        time.Now,
	}
}

func (i *Inspector) Inspect(ctx context.Context, req Request) (*CrashReport, error) {
	report := &CrashReport{
		PodName:     req.PodName,
		Namespace:   req.Namespace,
		ContextName: req.ContextName,
		GeneratedAt: i.now().UTC(),
	}

	pod, err := i.client.CoreV1().Pods(req.Namespace).Get(ctx, req.PodName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	statuses := collectStatuses(pod)
	selectedStatuses, err := filterStatuses(statuses, req.Container)
	if err != nil {
		return nil, err
	}

	baselineEntries, baselineWarnings := i.buildBaselineEntries(ctx, req, selectedStatuses)
	report.Warnings = append(report.Warnings, baselineWarnings...)

	eventEntries, eventWarnings, err := i.buildEventEntries(ctx, pod, statuses, req.Container)
	if err != nil {
		switch {
		case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
			report.Warnings = append(report.Warnings, fmt.Sprintf("Unable to list warning Events for %s/%s: %v", req.Namespace, req.PodName, err))
		default:
			return nil, err
		}
	} else {
		report.Warnings = append(report.Warnings, eventWarnings...)
		if len(eventEntries) == 0 && len(baselineEntries) > 0 {
			report.Warnings = append(report.Warnings, "Historical Events may have expired on this cluster; showing baseline pod termination state.")
		}
	}

	entries := mergeEntries(append(eventEntries, baselineEntries...))
	if req.Limit > 0 && len(entries) > req.Limit {
		entries = entries[:req.Limit]
	}

	report.Warnings = uniqueStrings(report.Warnings)
	report.Entries = entries
	return report, nil
}

func (i *Inspector) buildBaselineEntries(ctx context.Context, req Request, statuses []containerStatusRef) ([]CrashEntry, []string) {
	entries := make([]CrashEntry, 0, len(statuses))
	var warnings []string

	for _, status := range statuses {
		terminated := status.Status.LastTerminationState.Terminated
		if terminated == nil {
			continue
		}

		entry := CrashEntry{
			Container: status.Name,
			Timestamp: terminatedTimestamp(terminated).UTC(),
			Reason:    firstNonEmpty(terminated.Reason, "Terminated"),
			ExitCode:  intPtr(int(terminated.ExitCode)),
			Message:   strings.TrimSpace(terminated.Message),
			Source:    SourceLastTerminationState,
		}
		if entry.Message == "" && terminated.Signal != 0 {
			entry.Message = fmt.Sprintf("Terminated by signal %d.", terminated.Signal)
		}

		logs, source, warning := i.resolveTailLogs(ctx, req, status.Name)
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if logs != "" {
			entry.TailLogs = logs
			entry.TailLogSource = source
		}

		entries = append(entries, entry)
	}

	return entries, warnings
}

func (i *Inspector) buildEventEntries(ctx context.Context, pod *corev1.Pod, statuses []containerStatusRef, selectedContainer string) ([]CrashEntry, []string, error) {
	containerNames := namesForStatuses(statuses)
	items, warnings, err := i.listWarningEvents(ctx, pod.Namespace, string(pod.UID))
	if err != nil {
		return nil, warnings, err
	}

	entries := make([]CrashEntry, 0, len(items))
	implicitContainer := ""
	if selectedContainer != "" {
		implicitContainer = selectedContainer
	} else if len(containerNames) == 1 {
		implicitContainer = containerNames[0]
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
		if containerName == "" {
			containerName = implicitContainer
		}

		if selectedContainer != "" && containerName != selectedContainer {
			continue
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

func collectStatuses(pod *corev1.Pod) []containerStatusRef {
	statuses := make([]containerStatusRef, 0, len(pod.Status.InitContainerStatuses)+len(pod.Status.ContainerStatuses))

	for _, status := range pod.Status.InitContainerStatuses {
		statuses = append(statuses, containerStatusRef{
			Name:   status.Name,
			Status: status,
		})
	}

	for _, status := range pod.Status.ContainerStatuses {
		statuses = append(statuses, containerStatusRef{
			Name:   status.Name,
			Status: status,
		})
	}

	return statuses
}

func filterStatuses(statuses []containerStatusRef, selected string) ([]containerStatusRef, error) {
	if selected == "" {
		return statuses, nil
	}

	filtered := make([]containerStatusRef, 0, 1)
	for _, status := range statuses {
		if status.Name == selected {
			filtered = append(filtered, status)
		}
	}

	if len(filtered) == 0 {
		return nil, fmt.Errorf("container %q not found in pod", selected)
	}

	return filtered, nil
}

func namesForStatuses(statuses []containerStatusRef) []string {
	names := make([]string, 0, len(statuses))
	for _, status := range statuses {
		names = append(names, status.Name)
	}
	return names
}

func terminatedTimestamp(terminated *corev1.ContainerStateTerminated) time.Time {
	switch {
	case !terminated.FinishedAt.IsZero():
		return terminated.FinishedAt.Time
	case !terminated.StartedAt.IsZero():
		return terminated.StartedAt.Time
	default:
		return time.Unix(0, 0).UTC()
	}
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

func mergeEntries(entries []CrashEntry) []CrashEntry {
	sorted := append([]CrashEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Timestamp.Equal(sorted[j].Timestamp) {
			return sourcePriority(sorted[i]) < sourcePriority(sorted[j])
		}
		return sorted[i].Timestamp.After(sorted[j].Timestamp)
	})

	merged := make([]CrashEntry, 0, len(sorted))
	for _, entry := range sorted {
		if len(merged) == 0 {
			merged = append(merged, entry)
			continue
		}

		last := merged[len(merged)-1]
		if sameCrash(last, entry) {
			merged[len(merged)-1] = mergePair(last, entry)
			continue
		}

		merged = append(merged, entry)
	}

	return merged
}

func sameCrash(a, b CrashEntry) bool {
	if a.Source == b.Source || a.Container == "" || b.Container == "" || a.Container != b.Container {
		return false
	}

	delta := a.Timestamp.Sub(b.Timestamp)
	if delta < 0 {
		delta = -delta
	}
	if delta > 5*time.Second {
		return false
	}

	aReason := strings.ToLower(a.Reason)
	bReason := strings.ToLower(b.Reason)
	aMessage := strings.ToLower(a.Message)
	bMessage := strings.ToLower(b.Message)

	if aReason != "" && bReason != "" && aReason == bReason {
		return true
	}
	if aReason != "" && strings.Contains(bMessage, aReason) {
		return true
	}
	if bReason != "" && strings.Contains(aMessage, bReason) {
		return true
	}

	for _, entry := range []CrashEntry{a, b} {
		if entry.ExitCode == nil {
			continue
		}

		code := strconv.Itoa(*entry.ExitCode)
		if strings.Contains(aMessage, code) || strings.Contains(bMessage, code) {
			return true
		}
	}

	return false
}

func mergePair(a, b CrashEntry) CrashEntry {
	primary := a
	secondary := b
	if entryRichness(b) > entryRichness(a) {
		primary = b
		secondary = a
	}

	if secondary.Timestamp.After(primary.Timestamp) {
		primary.Timestamp = secondary.Timestamp
	}
	if primary.Container == "" {
		primary.Container = secondary.Container
	}
	if primary.Message == "" {
		primary.Message = secondary.Message
	}
	if primary.ExitCode == nil {
		primary.ExitCode = secondary.ExitCode
	}
	if primary.TailLogs == "" {
		primary.TailLogs = secondary.TailLogs
		primary.TailLogSource = secondary.TailLogSource
	} else if primary.TailLogSource == "" {
		primary.TailLogSource = secondary.TailLogSource
	}
	if primary.Source != SourceLastTerminationState && secondary.Source == SourceLastTerminationState {
		primary.Source = secondary.Source
	}

	return primary
}

func entryRichness(entry CrashEntry) int {
	score := 0
	if entry.Source == SourceLastTerminationState {
		score += 2
	}
	if entry.ExitCode != nil {
		score += 2
	}
	if entry.TailLogs != "" {
		score += 2
	}
	if entry.Message != "" {
		score++
	}
	return score
}

func (i *Inspector) resolveTailLogs(ctx context.Context, req Request, container string) (string, TailLogSource, string) {
	payload, err := i.logFetcher(ctx, req.Namespace, req.PodName, container, req.TailLines, true)
	if err == nil {
		payload = strings.TrimSpace(payload)
		switch reason := unavailableLogsReason(payload); {
		case reason != "":
			return i.fallbackToCurrentLogs(ctx, req, container, reason)
		case payload != "":
			return payload, TailLogSourcePrevious, ""
		default:
			return "", "", ""
		}
	}

	return i.fallbackToCurrentLogs(ctx, req, container, err.Error())
}

func (i *Inspector) fallbackToCurrentLogs(ctx context.Context, req Request, container, previousFailure string) (string, TailLogSource, string) {
	payload, err := i.logFetcher(ctx, req.Namespace, req.PodName, container, req.TailLines, false)
	if err != nil {
		return "", "", fmt.Sprintf(
			"Previous logs for container %q were unavailable: %s. Current log fallback failed: %v",
			container,
			previousFailure,
			err,
		)
	}

	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", "", fmt.Sprintf("Previous logs for container %q were unavailable: %s", container, previousFailure)
	}

	if reason := unavailableLogsReason(payload); reason != "" {
		return "", "", fmt.Sprintf(
			"Previous logs for container %q were unavailable: %s. Current log fallback was also unavailable: %s",
			container,
			previousFailure,
			reason,
		)
	}

	return payload, TailLogSourceCurrent, fmt.Sprintf(
		"Previous logs for container %q were unavailable: %s. Showing current container logs instead.",
		container,
		previousFailure,
	)
}

func sourcePriority(entry CrashEntry) int {
	if entry.Source == SourceLastTerminationState {
		return 0
	}
	return 1
}

func defaultLogFetcher(client kubernetes.Interface) logFetcher {
	return func(ctx context.Context, namespace, podName, container string, tailLines int64, previous bool) (string, error) {
		req := client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
			Container: container,
			Previous:  previous,
			TailLines: &tailLines,
		})

		stream, err := req.Stream(ctx)
		if err != nil {
			return "", err
		}
		defer stream.Close()

		payload, err := io.ReadAll(stream)
		if err != nil {
			return "", err
		}

		return strings.TrimSpace(string(payload)), nil
	}
}

func unavailableLogsReason(payload string) string {
	trimmed := strings.TrimSpace(payload)
	lower := strings.ToLower(trimmed)

	switch {
	case strings.Contains(lower, "unable to retrieve container logs for"):
		return trimmed
	case strings.Contains(lower, "previous terminated container"):
		return trimmed
	default:
		return ""
	}
}

func intPtr(v int) *int {
	return &v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
