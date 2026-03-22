package ui_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rohankaran/kubectl-crashloop/internal/crashloop"
	"github.com/rohankaran/kubectl-crashloop/internal/ui"
)

func TestRenderJSONProducesMachineReadableOutput(t *testing.T) {
	t.Parallel()

	report := crashloop.CrashReport{
		PodName:   "api-pod",
		Namespace: "prod",
		Entries: []crashloop.CrashEntry{{
			Container: "api",
			Timestamp: time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC),
			Reason:    "OOMKilled",
			Source:    crashloop.SourceLastTerminationState,
		}},
	}

	out, err := ui.Render(report, ui.RenderOptions{
		Format:    ui.OutputJSON,
		ColorMode: ui.ColorAlways,
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
				Container:     "api",
				Timestamp:     time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC),
				Reason:        "OOMKilled",
				ExitCode:      &exit137,
				Message:       "Exceeded memory limit",
				TailLogs:      "panic: out of memory",
				TailLogSource: crashloop.TailLogSourcePrevious,
				Source:        crashloop.SourceLastTerminationState,
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

	out, err := ui.Render(report, ui.RenderOptions{
		Format:    ui.OutputTable,
		ColorMode: ui.ColorNever,
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
	if !strings.Contains(out, "previous logs:") {
		t.Fatalf("expected previous logs section in output: %q", out)
	}
}

func TestRenderTableLabelsCurrentLogsFallback(t *testing.T) {
	t.Parallel()

	exit42 := 42
	report := crashloop.CrashReport{
		PodName:   "api-pod",
		Namespace: "prod",
		Entries: []crashloop.CrashEntry{{
			Container:     "api",
			Timestamp:     time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC),
			Reason:        "Error",
			ExitCode:      &exit42,
			TailLogs:      "starting\nfailing on purpose",
			TailLogSource: crashloop.TailLogSourceCurrent,
			Source:        crashloop.SourceLastTerminationState,
		}},
	}

	out, err := ui.Render(report, ui.RenderOptions{
		Format:    ui.OutputTable,
		ColorMode: ui.ColorNever,
		Width:     100,
		Writer:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(out, "current logs:") {
		t.Fatalf("expected current logs label in output: %q", out)
	}
}

func TestRenderTableHonorsNarrowWidths(t *testing.T) {
	t.Parallel()

	report := crashloop.CrashReport{
		PodName:   "api-pod",
		Namespace: "prod",
		Entries: []crashloop.CrashEntry{{
			Container: "api",
			Timestamp: time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC),
			Reason:    "BackOff",
			Message:   "Back-off restarting failed container api in pod api-pod_prod with a deliberately long message for wrapping",
			Source:    crashloop.SourceEvent,
		}},
	}

	width := 48
	out, err := ui.Render(report, ui.RenderOptions{
		Format:    ui.OutputTable,
		ColorMode: ui.ColorNever,
		Width:     width,
		Writer:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	for _, line := range strings.Split(out, "\n") {
		if utf8.RuneCountInString(line) > width {
			t.Fatalf("line exceeded width %d: %q", width, line)
		}
	}
}
