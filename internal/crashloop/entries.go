package crashloop

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

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

func filterStatusesForSelection(statuses []containerStatusRef, selected string) []containerStatusRef {
	if selected == "" {
		return statuses
	}

	filtered := make([]containerStatusRef, 0, 1)
	for _, status := range statuses {
		if status.Name == selected {
			filtered = append(filtered, status)
		}
	}

	return filtered
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

	if reasonsStronglyMatch(aReason, bReason) {
		return true
	}
	if reasonExplainsMessage(aReason, bMessage) {
		return true
	}
	if reasonExplainsMessage(bReason, aMessage) {
		return true
	}

	for _, entry := range []CrashEntry{a, b} {
		if entry.ExitCode == nil {
			continue
		}

		if explicitlyMentionsExitCode(aMessage, *entry.ExitCode) || explicitlyMentionsExitCode(bMessage, *entry.ExitCode) {
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

func reasonsStronglyMatch(aReason, bReason string) bool {
	if aReason == "" || bReason == "" || aReason != bReason {
		return false
	}

	return isSpecificMergeReason(aReason)
}

func reasonExplainsMessage(reason, message string) bool {
	if !isSpecificMergeReason(reason) || message == "" {
		return false
	}

	return strings.Contains(message, reason)
}

func isSpecificMergeReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "", "error", "warning", "terminated", "backoff", "crashloopbackoff":
		return false
	default:
		return true
	}
}

func explicitlyMentionsExitCode(message string, code int) bool {
	if strings.TrimSpace(message) == "" {
		return false
	}

	codeString := strconv.Itoa(code)
	for _, phrase := range []string{
		"exit code " + codeString,
		"exit status " + codeString,
		"exited with code " + codeString,
		"returned " + codeString,
	} {
		if strings.Contains(message, phrase) {
			return true
		}
	}

	return false
}

func sourcePriority(entry CrashEntry) int {
	if entry.Source == SourceLastTerminationState {
		return 0
	}
	return 1
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
