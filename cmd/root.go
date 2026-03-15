package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"

	"github.com/rohankaran/kubectl-crashloop/internal/crashloop"
	"github.com/rohankaran/kubectl-crashloop/internal/ui"
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

func Execute() error {
	return newRootCmd(defaultDependencies()).Execute()
}

func defaultDependencies() commandDependencies {
	return commandDependencies{
		inspect: runInspect,
		demo:    crashloop.DemoReport,
		version: version.Get(),
	}
}

func newRootCmd(deps commandDependencies) *cobra.Command {
	opts := rootOptions{}
	configFlags := genericclioptions.NewConfigFlags(true)

	rootCmd := &cobra.Command{
		Use:           "kubectl-crashloop POD",
		Short:         "Inspect pod crash history, restart warnings, and previous logs",
		Long:          "kubectl-crashloop correlates warning Events, last termination state, and previous container logs into a single readable report.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}

			return runRender(
				cmd,
				deps.inspect,
				configFlags,
				opts,
				args[0],
				ui.WidthAuto,
				func() ui.ColorMode {
					mode, _ := ui.ParseColorMode(opts.color)
					return mode
				},
			)
		},
	}

	configFlags.AddFlags(rootCmd.PersistentFlags())
	rootCmd.PersistentFlags().StringVarP(&opts.container, "container", "c", "", "Container name to inspect")
	rootCmd.PersistentFlags().Int64Var(&opts.tailLines, "tail", 5, "Number of previous log lines to fetch per container")
	rootCmd.PersistentFlags().IntVar(&opts.limit, "limit", 10, "Maximum number of crash entries to display")
	rootCmd.PersistentFlags().StringVarP(&opts.output, "output", "o", string(ui.OutputTable), "Output format: table or json")
	rootCmd.PersistentFlags().StringVar(&opts.color, "color", string(ui.ColorAuto), "Color mode: auto, always, or never")

	rootCmd.AddCommand(newDemoCmd(deps, configFlags, &opts))
	rootCmd.AddCommand(newVersionCmd(deps.version))

	return rootCmd
}

func newDemoCmd(deps commandDependencies, configFlags *genericclioptions.ConfigFlags, opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:           "demo",
		Short:         "Render a deterministic demo report for README screenshots and VHS tapes",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			report := deps.demo()
			format, err := ui.ParseOutputFormat(opts.output)
			if err != nil {
				return err
			}

			rendered, err := ui.Render(report, ui.RenderOptions{
				Format:    format,
				ColorMode: ui.ColorAlways,
				Width:     100,
				Writer:    cmd.OutOrStdout(),
			})
			if err != nil {
				return err
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), rendered)
			return err
		},
	}
}

func newVersionCmd(info version.Info) *cobra.Command {
	return &cobra.Command{
		Use:           "version",
		Short:         "Print build metadata",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(
				cmd.OutOrStdout(),
				"kubectl-crashloop %s\ncommit: %s\ndate: %s\n",
				info.Version,
				info.Commit,
				info.Date,
			)
			return err
		},
	}
}

func runRender(
	cmd *cobra.Command,
	inspect inspectFunc,
	configFlags *genericclioptions.ConfigFlags,
	opts rootOptions,
	podName string,
	width int,
	resolveColor func() ui.ColorMode,
) error {
	if opts.tailLines <= 0 {
		return fmt.Errorf("--tail must be greater than 0")
	}
	if opts.limit <= 0 {
		return fmt.Errorf("--limit must be greater than 0")
	}

	format, err := ui.ParseOutputFormat(opts.output)
	if err != nil {
		return err
	}

	colorMode, err := ui.ParseColorMode(opts.color)
	if err != nil {
		return err
	}

	if resolveColor != nil {
		colorMode = resolveColor()
	}

	report, err := inspect(cmd.Context(), configFlags, inspectOptions{
		PodName:   podName,
		Container: opts.container,
		TailLines: opts.tailLines,
		Limit:     opts.limit,
	})
	if err != nil {
		return err
	}

	if width == ui.WidthAuto {
		width = detectOutputWidth(cmd.OutOrStdout(), 100)
	}

	rendered, err := ui.Render(*report, ui.RenderOptions{
		Format:    format,
		ColorMode: colorMode,
		Width:     width,
		Writer:    cmd.OutOrStdout(),
	})
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), rendered)
	return err
}

func runInspect(ctx context.Context, configFlags *genericclioptions.ConfigFlags, opts inspectOptions) (*crashloop.CrashReport, error) {
	restConfig, err := configFlags.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	restConfig.UserAgent = "kubectl-crashloop/" + version.Get().Version

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	rawLoader := configFlags.ToRawKubeConfigLoader()
	namespace, _, err := rawLoader.Namespace()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(namespace) == "" {
		namespace = "default"
	}

	rawConfig, err := rawLoader.RawConfig()
	if err != nil {
		return nil, err
	}

	inspector := crashloop.NewInspector(clientset)
	return inspector.Inspect(ctx, crashloop.Request{
		Namespace:   namespace,
		ContextName: rawConfig.CurrentContext,
		PodName:     opts.PodName,
		Container:   opts.Container,
		TailLines:   opts.TailLines,
		Limit:       opts.Limit,
	})
}

func detectOutputWidth(w io.Writer, fallback int) int {
	type fdWriter interface {
		Fd() uintptr
	}

	fd, ok := w.(fdWriter)
	if !ok {
		return fallback
	}

	width, _, err := term.GetSize(int(fd.Fd()))
	if err != nil || width <= 0 {
		return fallback
	}

	return width
}
