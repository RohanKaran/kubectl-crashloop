package crashloop

import "time"

type Source string

const (
	SourceEvent                Source = "event"
	SourceLastTerminationState Source = "lastTerminationState"
)

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
	Container string    `json:"container,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason"`
	ExitCode  *int      `json:"exitCode,omitempty"`
	Message   string    `json:"message,omitempty"`
	TailLogs  string    `json:"tailLogs,omitempty"`
	Source    Source    `json:"source"`
}
