package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent  = lipgloss.Color("69")  // blue
	colorLive    = lipgloss.Color("204") // pink/red
	colorGood    = lipgloss.Color("42")  // green
	colorMid     = lipgloss.Color("214") // orange
	colorBad     = lipgloss.Color("196") // red
	colorMuted   = lipgloss.Color("244") // gray
	colorAddFg   = lipgloss.Color("42")
	colorDelFg   = lipgloss.Color("203")
	colorHunkFg  = lipgloss.Color("75")
	colorBorder  = lipgloss.Color("240")
	colorSelText = lipgloss.Color("231")
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	selectedRowStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSelText).Background(colorAccent)

	mutedStyle = lipgloss.NewStyle().Foreground(colorMuted)

	liveBadgeStyle = lipgloss.NewStyle().Bold(true).Foreground(colorLive)

	paneActiveBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent)

	paneInactiveBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorder)

	footerStyle = lipgloss.NewStyle().Foreground(colorMuted)

	addLineStyle  = lipgloss.NewStyle().Foreground(colorAddFg)
	delLineStyle  = lipgloss.NewStyle().Foreground(colorDelFg)
	hunkLineStyle = lipgloss.NewStyle().Foreground(colorHunkFg)

	modalBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2)

	yesStyle = lipgloss.NewStyle().Bold(true).Foreground(colorGood)
	noStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorBad)
)

// scoreStyle returns a foreground style colored for a 0..1 score.
func scoreStyle(s float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(scoreColor(s))
}

// scoreColor returns a color for a 0..1 score.
func scoreColor(s float64) lipgloss.Color {
	switch {
	case s < 0:
		return colorMuted
	case s >= 0.8:
		return colorGood
	case s >= 0.5:
		return colorMid
	default:
		return colorBad
	}
}
