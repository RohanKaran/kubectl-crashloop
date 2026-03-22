package ui

import (
	"io"
	"time"

	"github.com/charmbracelet/lipgloss"

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
