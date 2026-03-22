package cmd

import (
	"context"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/rohankaran/kubectl-crashloop/internal/crashloop"
	"github.com/rohankaran/kubectl-crashloop/internal/version"
)

type inspectOptions struct {
	PodName   string
	Container string
	TailLines int64
	Limit     int
}

type inspectFunc func(context.Context, *genericclioptions.ConfigFlags, inspectOptions) (*crashloop.CrashReport, error)

type commandDependencies struct {
	inspect inspectFunc
	demo    func() crashloop.CrashReport
	version version.Info
}

type rootOptions struct {
	container string
	tailLines int64
	limit     int
	output    string
	color     string
}
