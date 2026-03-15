package ui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rohankaran/kubectl-crashloop/internal/crashloop"
)

func TestRenderJSONProducesMachineReadableOutput(t *testing.T) {
	t.Parallel()

	report := crashloop.CrashReport{
		PodName:   "api-pod",
		Namespace: "prod",
		Entries: []crashloop.CrashEntry{
			{
				Container: "api",
				Timestamp: time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC),
				Reason:    "OOMKilled",
				Source:    crashloop.SourceLastTerminationState,
			},
		},
	}

	out, err := Render(report, RenderOptions{
		Format:    OutputJSON,
		ColorMode: ColorAlways,
		Width:     100,
		Writer:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if strings.Contains(out, "\x1b[") {
		t.Fatalf("json output contained ANSI sequences: %q", out)
	}

	var decoded crashloop.CrashReport
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if decoded.PodName != "api-pod" {
		t.Fatalf("decoded podName = %q, want api-pod", decoded.PodName)
	}
}

func TestRenderTableGroupsEntriesWithoutANSIWhenColorNever(t *testing.T) {
	t.Parallel()

	exit137 := 137
	report := crashloop.CrashReport{
		PodName:     "api-pod",
		Namespace:   "prod",
		ContextName: "kind-prod",
		Warnings:    []string{"Historical Events may have expired on this cluster."},
		Entries: []crashloop.CrashEntry{
			{
				Container: "api",
				Timestamp: time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC),
				Reason:    "OOMKilled",
				ExitCode:  &exit137,
				Message:   "Exceeded memory limit",
				TailLogs:  "panic: out of memory",
				Source:    crashloop.SourceLastTerminationState,
			},
			{
				Container: "worker",
				Timestamp: time.Date(2026, 3, 15, 11, 58, 0, 0, time.UTC),
				Reason:    "BackOff",
				Message:   "Back-off restarting failed container worker",
				Source:    crashloop.SourceEvent,
			},
		},
	}

	out, err := Render(report, RenderOptions{
		Format:    OutputTable,
		ColorMode: ColorNever,
		Width:     100,
		Writer:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if strings.Contains(out, "\x1b[") {
		t.Fatalf("table output contained ANSI sequences: %q", out)
	}
	if !strings.Contains(out, "container: api") || !strings.Contains(out, "container: worker") {
		t.Fatalf("group headings missing from table output: %q", out)
	}
	if !strings.Contains(out, "logs:") {
		t.Fatalf("expected logs section in output: %q", out)
	}
}
