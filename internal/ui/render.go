package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/muesli/termenv"

	"github.com/rohankaran/kubectl-crashloop/internal/crashloop"
)

const WidthAuto = 0

type OutputFormat string

const (
	OutputTable OutputFormat = "table"
	OutputJSON  OutputFormat = "json"
)

type ColorMode string

const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

type RenderOptions struct {
	Format    OutputFormat
	ColorMode ColorMode
	Width     int
	Writer    io.Writer
}

type group struct {
	label     string
	timestamp time.Time
	entries   []crashloop.CrashEntry
}

type theme struct {
	renderer      *lipgloss.Renderer
	headerBox     lipgloss.Style
	title         lipgloss.Style
	meta          lipgloss.Style
	warning       lipgloss.Style
	empty         lipgloss.Style
	section       lipgloss.Style
	border        lipgloss.Style
	headerCell    lipgloss.Style
	oddCell       lipgloss.Style
	evenCell      lipgloss.Style
	timeCell      lipgloss.Style
	detailsCell   lipgloss.Style
	sourceEvent   lipgloss.Style
	sourceLast    lipgloss.Style
	reasonError   lipgloss.Style
	reasonWarning lipgloss.Style
}

func ParseOutputFormat(raw string) (OutputFormat, error) {
	switch OutputFormat(strings.ToLower(strings.TrimSpace(raw))) {
	case OutputTable:
		return OutputTable, nil
	case OutputJSON:
		return OutputJSON, nil
	default:
		return "", fmt.Errorf("unsupported output format %q", raw)
	}
}

func ParseColorMode(raw string) (ColorMode, error) {
	switch ColorMode(strings.ToLower(strings.TrimSpace(raw))) {
	case ColorAuto:
		return ColorAuto, nil
	case ColorAlways:
		return ColorAlways, nil
	case ColorNever:
		return ColorNever, nil
	default:
		return "", fmt.Errorf("unsupported color mode %q", raw)
	}
}

func Render(report crashloop.CrashReport, opts RenderOptions) (string, error) {
	switch opts.Format {
	case OutputJSON:
		payload, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return "", err
		}
		return string(payload), nil
	case OutputTable:
	default:
		return "", fmt.Errorf("unsupported output format %q", opts.Format)
	}

	if opts.Width <= 0 {
		opts.Width = 100
	}

	renderer := lipgloss.NewRenderer(opts.Writer)
	switch opts.ColorMode {
	case ColorAlways:
		renderer.SetColorProfile(termenv.TrueColor)
	case ColorNever:
		renderer.SetColorProfile(termenv.Ascii)
	}

	theme := buildTheme(renderer)
	sections := []string{
		renderHeader(theme, report, opts.Width),
	}

	for _, warning := range report.Warnings {
		sections = append(sections, theme.warning.Render("Warning: "+warning))
	}

	if len(report.Entries) == 0 {
		sections = append(sections, theme.empty.Render(
			fmt.Sprintf("No crash history found for pod %s in namespace %s.", report.PodName, report.Namespace),
		))
		return strings.Join(sections, "\n\n"), nil
	}

	for _, group := range groupEntries(report.Entries) {
		sections = append(sections, renderGroup(theme, group, opts.Width))
	}

	return strings.Join(sections, "\n\n"), nil
}

func buildTheme(renderer *lipgloss.Renderer) theme {
	title := renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC"))
	meta := renderer.NewStyle().Foreground(lipgloss.Color("#CBD5E1"))
	border := renderer.NewStyle().Foreground(lipgloss.Color("#475569"))

	return theme{
		renderer: renderer,
		headerBox: renderer.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#334155")).
			Background(lipgloss.Color("#0F172A")).
			Padding(1, 2),
		title:   title,
		meta:    meta,
		warning: renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("#F59E0B")),
		empty: renderer.NewStyle().
			Foreground(lipgloss.Color("#94A3B8")).
			Italic(true),
		section: renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("#E2E8F0")),
		border:  border,
		headerCell: renderer.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#E2E8F0")).
			Background(lipgloss.Color("#1E293B")).
			Padding(0, 1),
		oddCell: renderer.NewStyle().
			Foreground(lipgloss.Color("#CBD5E1")).
			Padding(0, 1),
		evenCell: renderer.NewStyle().
			Foreground(lipgloss.Color("#E2E8F0")).
			Padding(0, 1),
		timeCell: renderer.NewStyle().
			Foreground(lipgloss.Color("#94A3B8")).
			Padding(0, 1),
		detailsCell: renderer.NewStyle().
			Foreground(lipgloss.Color("#CBD5E1")).
			Padding(0, 1),
		sourceEvent: renderer.NewStyle().
			Foreground(lipgloss.Color("#F8FAFC")).
			Background(lipgloss.Color("#475569")).
			Padding(0, 1),
		sourceLast: renderer.NewStyle().
			Foreground(lipgloss.Color("#F8FAFC")).
			Background(lipgloss.Color("#0F766E")).
			Padding(0, 1),
		reasonError: renderer.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F87171")).
			Padding(0, 1),
		reasonWarning: renderer.NewStyle().
			Foreground(lipgloss.Color("#FBBF24")).
			Padding(0, 1),
	}
}

func renderHeader(theme theme, report crashloop.CrashReport, width int) string {
	meta := []string{
		fmt.Sprintf("pod %s", report.PodName),
		fmt.Sprintf("namespace %s", report.Namespace),
	}
	if strings.TrimSpace(report.ContextName) != "" {
		meta = append(meta, fmt.Sprintf("context %s", report.ContextName))
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		theme.title.Render("kubectl-crashloop"),
		theme.meta.Render(strings.Join(meta, "  •  ")),
	)

	return theme.headerBox.MaxWidth(width).Render(content)
}

func renderGroup(theme theme, group group, width int) string {
	rows := make([][]string, 0, len(group.entries))
	for _, entry := range group.entries {
		rows = append(rows, []string{
			entry.Timestamp.Format("2006-01-02 15:04:05Z07:00"),
			entry.Reason,
			renderExitCode(entry),
			renderSource(entry),
			renderDetails(entry),
		})
	}

	tbl := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(theme.border).
		BorderHeader(true).
		Headers("TIME", "REASON", "EXIT", "SRC", "DETAILS").
		Rows(rows...).
		Width(maxInt(width, 72)).
		Wrap(true).
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == table.HeaderRow:
				return theme.headerCell
			default:
				entry := group.entries[row]
				return styleForCell(theme, entry, row, col)
			}
		})

	return lipgloss.JoinVertical(
		lipgloss.Left,
		theme.section.Render(group.label),
		tbl.String(),
	)
}

func styleForCell(theme theme, entry crashloop.CrashEntry, row, col int) lipgloss.Style {
	base := theme.oddCell
	if row%2 == 0 {
		base = theme.evenCell
	}

	switch col {
	case 0:
		return theme.timeCell
	case 1:
		if entry.ExitCode != nil && *entry.ExitCode == 137 {
			return theme.reasonError
		}
		return theme.reasonWarning
	case 3:
		if entry.Source == crashloop.SourceLastTerminationState {
			return theme.sourceLast
		}
		return theme.sourceEvent
	case 4:
		return theme.detailsCell
	default:
		return base
	}
}

func renderExitCode(entry crashloop.CrashEntry) string {
	if entry.ExitCode == nil {
		return "n/a"
	}
	return fmt.Sprintf("%d", *entry.ExitCode)
}

func renderSource(entry crashloop.CrashEntry) string {
	if entry.Source == crashloop.SourceLastTerminationState {
		return "state"
	}
	return "event"
}

func renderDetails(entry crashloop.CrashEntry) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(entry.Message) != "" {
		parts = append(parts, entry.Message)
	}
	if strings.TrimSpace(entry.TailLogs) != "" {
		parts = append(parts, renderTailLogsLabel(entry)+":\n"+entry.TailLogs)
	}
	if len(parts) == 0 {
		return "n/a"
	}
	return strings.Join(parts, "\n")
}

func renderTailLogsLabel(entry crashloop.CrashEntry) string {
	switch entry.TailLogSource {
	case crashloop.TailLogSourceCurrent:
		return "current logs"
	case crashloop.TailLogSourcePrevious:
		return "previous logs"
	default:
		return "logs"
	}
}

func groupEntries(entries []crashloop.CrashEntry) []group {
	buckets := make(map[string][]crashloop.CrashEntry)
	latest := make(map[string]time.Time)

	for _, entry := range entries {
		label := entry.Container
		if label == "" {
			label = "pod-wide events"
		} else {
			label = "container: " + label
		}

		buckets[label] = append(buckets[label], entry)
		if entry.Timestamp.After(latest[label]) {
			latest[label] = entry.Timestamp
		}
	}

	groups := make([]group, 0, len(buckets))
	for label, bucket := range buckets {
		sortEntries(bucket)
		groups = append(groups, group{
			label:     label,
			timestamp: latest[label],
			entries:   bucket,
		})
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].timestamp.After(groups[j].timestamp)
	})

	return groups
}

func sortEntries(entries []crashloop.CrashEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
