package crashloop

import (
	"context"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

func (i *Inspector) buildBaselineEntries(ctx context.Context, logRequest podLogRequest, statuses []containerStatusRef) ([]CrashEntry, []string) {
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

		logs, source, warning := i.resolveTailLogs(ctx, logRequest, status.Name)
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

func (i *Inspector) attachCurrentLogsToEventEntries(
	ctx context.Context,
	logRequest podLogRequest,
	statuses []containerStatusRef,
	baselineEntries []CrashEntry,
	eventEntries []CrashEntry,
) ([]CrashEntry, []string) {
	if len(eventEntries) == 0 {
		return eventEntries, nil
	}

	baselineContainers := make(map[string]struct{}, len(baselineEntries))
	for _, entry := range baselineEntries {
		if strings.TrimSpace(entry.Container) == "" {
			continue
		}
		baselineContainers[entry.Container] = struct{}{}
	}

	latestEventIndex := make(map[string]int, len(eventEntries))
	for idx, entry := range eventEntries {
		if strings.TrimSpace(entry.Container) == "" {
			continue
		}

		currentIdx, ok := latestEventIndex[entry.Container]
		if !ok || eventEntries[currentIdx].Timestamp.Before(entry.Timestamp) {
			latestEventIndex[entry.Container] = idx
		}
	}

	var warnings []string
	for _, status := range statuses {
		if _, ok := baselineContainers[status.Name]; ok {
			continue
		}

		idx, ok := latestEventIndex[status.Name]
		if !ok {
			continue
		}
		if strings.TrimSpace(eventEntries[idx].TailLogs) != "" {
			continue
		}

		logs, warning := i.resolveCurrentLogsForEvent(ctx, logRequest, status.Name)
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if logs == "" {
			continue
		}

		eventEntries[idx].TailLogs = logs
		eventEntries[idx].TailLogSource = TailLogSourceCurrent
	}

	return eventEntries, warnings
}

func (i *Inspector) resolveCurrentLogsForEvent(ctx context.Context, logRequest podLogRequest, container string) (string, string) {
	payload, err := i.logFetcher(ctx, logRequest.namespace, logRequest.podName, container, logRequest.tailLines, false)
	if err != nil {
		return "", fmt.Sprintf(
			"No last termination state was available for container %q, and current logs could not be fetched: %v",
			container,
			err,
		)
	}

	if payload == "" {
		return "", fmt.Sprintf("No last termination state was available for container %q, and current logs were empty.", container)
	}

	if reason := unavailableLogsReason(payload); reason != "" {
		return "", fmt.Sprintf(
			"No last termination state was available for container %q, and current logs were unavailable: %s",
			container,
			reason,
		)
	}

	return payload, fmt.Sprintf(
		"No last termination state was available for container %q; showing current container logs on the latest warning event.",
		container,
	)
}

func (i *Inspector) resolveTailLogs(ctx context.Context, logRequest podLogRequest, container string) (string, TailLogSource, string) {
	payload, err := i.logFetcher(ctx, logRequest.namespace, logRequest.podName, container, logRequest.tailLines, true)
	if err == nil {
		switch reason := unavailableLogsReason(payload); {
		case reason != "":
			return i.fallbackToCurrentLogs(ctx, logRequest, container, reason)
		case payload != "":
			return payload, TailLogSourcePrevious, ""
		default:
			return "", "", ""
		}
	}

	return i.fallbackToCurrentLogs(ctx, logRequest, container, err.Error())
}

func (i *Inspector) fallbackToCurrentLogs(ctx context.Context, logRequest podLogRequest, container, previousFailure string) (string, TailLogSource, string) {
	payload, err := i.logFetcher(ctx, logRequest.namespace, logRequest.podName, container, logRequest.tailLines, false)
	if err != nil {
		return "", "", fmt.Sprintf(
			"Previous logs for container %q were unavailable: %s. Current log fallback failed: %v",
			container,
			previousFailure,
			err,
		)
	}

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
		defer func() {
			_ = stream.Close()
		}()

		payload, err := io.ReadAll(stream)
		if err != nil {
			return "", err
		}

		return strings.TrimSpace(string(payload)), nil
	}
}

func unavailableLogsReason(payload string) string {
	lower := strings.ToLower(payload)

	switch {
	case strings.Contains(lower, "unable to retrieve container logs for"):
		return payload
	case strings.Contains(lower, "previous terminated container"):
		return payload
	default:
		return ""
	}
}
