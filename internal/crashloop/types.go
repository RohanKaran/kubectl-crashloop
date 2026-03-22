package crashloop

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
)

type Source string

const (
	SourceEvent                Source = "event"
	SourceLastTerminationState Source = "lastTerminationState"
)

type TailLogSource string

const (
	TailLogSourcePrevious TailLogSource = "previous"
	TailLogSourceCurrent  TailLogSource = "current"
)

type logFetcher func(context.Context, string, string, string, int64, bool) (string, error)

type InspectorOption func(*Inspector)

type Request struct {
	Namespace   string
	ContextName string
	PodName     string
	Container   string
	TailLines   int64
	Limit       int
}

type CrashReport struct {
	PodName     string       `json:"podName"`
	Namespace   string       `json:"namespace"`
	ContextName string       `json:"context,omitempty"`
	GeneratedAt time.Time    `json:"generatedAt"`
	Warnings    []string     `json:"warnings,omitempty"`
	Entries     []CrashEntry `json:"entries"`
}

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
