package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/rohankaran/kubectl-crashloop/internal/crashloop"
	"github.com/rohankaran/kubectl-crashloop/internal/version"
)

func TestRootCommandPassesFlagsToInspect(t *testing.T) {
	t.Parallel()

	var captured inspectOptions
	var capturedNamespace string

	cmd := newRootCmd(commandDependencies{
		inspect: func(_ context.Context, flags *genericclioptions.ConfigFlags, opts inspectOptions) (*crashloop.CrashReport, error) {
			captured = opts
			if flags.Namespace != nil {
				capturedNamespace = *flags.Namespace
			}

			return &crashloop.CrashReport{
				PodName:   opts.PodName,
				Namespace: capturedNamespace,
				Entries: []crashloop.CrashEntry{
					{
						Container: "api",
						Reason:    "OOMKilled",
						Source:    crashloop.SourceLastTerminationState,
					},
				},
			}, nil
		},
		demo: crashloop.DemoReport,
		version: version.Info{
			Version: "test",
			Commit:  "abc123",
			Date:    "2026-03-15T00:00:00Z",
		},
	})

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{
		"api-pod",
		"-n", "prod",
		"-c", "api",
		"--tail", "8",
		"--limit", "3",
		"-o", "json",
		"--color", "never",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if captured.PodName != "api-pod" {
		t.Fatalf("captured pod = %q, want api-pod", captured.PodName)
	}
	if captured.Container != "api" {
		t.Fatalf("captured container = %q, want api", captured.Container)
	}
	if captured.TailLines != 8 {
		t.Fatalf("captured tail = %d, want 8", captured.TailLines)
	}
	if captured.Limit != 3 {
		t.Fatalf("captured limit = %d, want 3", captured.Limit)
	}
	if capturedNamespace != "prod" {
		t.Fatalf("captured namespace = %q, want prod", capturedNamespace)
	}

	var report crashloop.CrashReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if report.Namespace != "prod" {
		t.Fatalf("report namespace = %q, want prod", report.Namespace)
	}
}

func TestRootCommandRejectsInvalidColor(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd(commandDependencies{
		inspect: func(context.Context, *genericclioptions.ConfigFlags, inspectOptions) (*crashloop.CrashReport, error) {
			t.Fatal("inspect should not be called when color parsing fails")
			return nil, nil
		},
		demo: crashloop.DemoReport,
		version: version.Info{
			Version: "test",
			Commit:  "abc123",
			Date:    "2026-03-15T00:00:00Z",
		},
	})

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"api-pod", "--color", "broken"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want invalid color error")
	}
	if !strings.Contains(err.Error(), "unsupported color mode") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestDemoCommandForcesColor(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd(commandDependencies{
		inspect: func(context.Context, *genericclioptions.ConfigFlags, inspectOptions) (*crashloop.CrashReport, error) {
			t.Fatal("inspect should not be called for demo")
			return nil, nil
		},
		demo: crashloop.DemoReport,
		version: version.Info{
			Version: "test",
			Commit:  "abc123",
			Date:    "2026-03-15T00:00:00Z",
		},
	})

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"demo"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "\x1b[") {
		t.Fatalf("demo output did not contain ANSI sequences: %q", stdout.String())
	}
}

func TestVersionCommandOutputsBuildInfo(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd(commandDependencies{
		inspect: func(context.Context, *genericclioptions.ConfigFlags, inspectOptions) (*crashloop.CrashReport, error) {
			t.Fatal("inspect should not be called for version")
			return nil, nil
		},
		demo: crashloop.DemoReport,
		version: version.Info{
			Version: "v0.1.0",
			Commit:  "abc123",
			Date:    "2026-03-15T00:00:00Z",
		},
	})

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "v0.1.0") || !strings.Contains(out, "abc123") {
		t.Fatalf("unexpected version output: %q", out)
	}
}

func TestRootCommandWithoutArgsShowsHelp(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd(commandDependencies{
		inspect: func(context.Context, *genericclioptions.ConfigFlags, inspectOptions) (*crashloop.CrashReport, error) {
			t.Fatal("inspect should not be called without pod args")
			return nil, nil
		},
		demo: crashloop.DemoReport,
		version: version.Info{
			Version: "test",
			Commit:  "abc123",
			Date:    "2026-03-15T00:00:00Z",
		},
	})

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("expected help output, got %q", stdout.String())
	}
}
