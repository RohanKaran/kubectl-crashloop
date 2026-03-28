// Package crashloop inspects pod crash history by correlating Events,
// termination state, and container logs.
package crashloop

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Source identifies where a crash entry originated.
type Source string

// Crash entry sources.
const (
	SourceEvent                Source = "event"
	SourceNodeEvent            Source = "nodeEvent"
	SourcePodStatus            Source = "podStatus"
	SourceLastTerminationState Source = "lastTerminationState"
)

// TailLogSource identifies which log stream was attached to an entry.
type TailLogSource string

// Tail log sources.
const (
	TailLogSourcePrevious TailLogSource = "previous"
	TailLogSourceCurrent  TailLogSource = "current"
)

type logFetcher func(context.Context, string, string, string, int64, bool) (string, error)

// InspectorOption customizes an Inspector during construction.
type InspectorOption func(*Inspector)

// Request describes which pod crash history to inspect.
type Request struct {
	Namespace   string
	ContextName string
	PodName     string
	Container   string
	TailLines   int64
	Limit       int
}

// CrashReport is the rendered data model for a pod crash inspection.
type CrashReport struct {
	PodName     string       `json:"podName"`
	Namespace   string       `json:"namespace"`
	ContextName string       `json:"context,omitempty"`
	GeneratedAt time.Time    `json:"generatedAt"`
	Warnings    []string     `json:"warnings,omitempty"`
	Entries     []CrashEntry `json:"entries"`
}

// CrashEntry describes one crash-related signal for a pod or container.
type CrashEntry struct {
	Container     string        `json:"container,omitempty"`
	Timestamp     time.Time     `json:"timestamp"`
	Reason        string        `json:"reason"`
	ExitCode      *int          `json:"exitCode,omitempty"`
	Message       string        `json:"message,omitempty"`
	TailLogs      string        `json:"tailLogs,omitempty"`
	TailLogSource TailLogSource `json:"tailLogSource,omitempty"`
	Source        Source        `json:"source"`
}

type containerStatusRef struct {
	Name   string
	Status corev1.ContainerStatus
}

type podLogRequest struct {
	namespace string
	podName   string
	tailLines int64
}
