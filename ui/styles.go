package ui

import "github.com/charmbracelet/lipgloss"

var (
	ColorPrimary   = lipgloss.Color("#7C3AED")
	ColorSecondary = lipgloss.Color("#06B6D4")
	ColorSuccess   = lipgloss.Color("#10B981")
	ColorWarning   = lipgloss.Color("#F59E0B")
	ColorDanger    = lipgloss.Color("#EF4444")
	ColorMuted     = lipgloss.Color("#6B7280")
	ColorText      = lipgloss.Color("#F9FAFB")
	ColorSubtle    = lipgloss.Color("#9CA3AF")
	ColorDimBorder = lipgloss.Color("#2D2D4E")

	ActiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary)

	InactiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorMuted)

	PanelTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary)

	InactivePanelTitle = lipgloss.NewStyle().
				Foreground(ColorMuted)

	SelectedItem = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	NormalItem = lipgloss.NewStyle().
			Foreground(ColorText)

	MutedItem = lipgloss.NewStyle().
			Foreground(ColorMuted)

	WorkingBadge = lipgloss.NewStyle().
			Foreground(ColorWarning).
			Bold(true)

	DoneBadge = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	DeadBadge = lipgloss.NewStyle().
			Foreground(ColorDanger).
			Bold(true)

	TimestampStyle = lipgloss.NewStyle().
			Foreground(ColorSubtle)

	StatusBar = lipgloss.NewStyle().
			Foreground(ColorSubtle).
			Background(lipgloss.Color("#1F2937")).
			Padding(0, 1)

	HelpKey = lipgloss.NewStyle().
		Foreground(ColorSecondary)

	HelpDesc = lipgloss.NewStyle().
			Foreground(ColorMuted)

	HelpSep = lipgloss.NewStyle().
		Foreground(ColorMuted)

	OverlayStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorWarning).
			Padding(1, 3)

	OverlayTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorWarning)

	InputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Padding(0, 1)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorDanger).
			Bold(true)

	PreviewKey = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	PreviewValue = lipgloss.NewStyle().
			Foreground(ColorText)

	PreviewComment = lipgloss.NewStyle().
			Foreground(ColorSubtle)

	// Card border styles (for dashboard)
	CardIdle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorMuted).
			Padding(0, 1)

	CardWorking = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorWarning).
			Padding(0, 1)

	CardDone = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorSuccess).
			Padding(0, 1)

	CardStopped = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDimBorder).
			Padding(0, 1)

	CardSelected = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Padding(0, 1)
)
