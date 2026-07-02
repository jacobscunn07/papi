package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Palette — a "lab instrument" identity. The accent (instrument cyan) deliberately
// avoids the green/amber/red score hues so the chrome never collides with the
// fitness signal. AdaptiveColor pairs keep it legible on light and dark terminals.
var (
	colorAccent  = lipgloss.AdaptiveColor{Light: "#0E8E7F", Dark: "#46D9C7"} // instrument cyan
	colorLive    = lipgloss.AdaptiveColor{Light: "#C2298E", Dark: "#FF6DC8"} // live/running only
	colorGood    = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"} // score >= .8
	colorMid     = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"} // score >= .5
	colorBad     = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#E5534B"} // score < .5
	colorMuted   = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#8B949E"} // chrome/labels
	colorBorder  = lipgloss.AdaptiveColor{Light: "#D0D7DE", Dark: "#30363D"} // inactive borders
	colorSelText = lipgloss.AdaptiveColor{Light: "#0B1416", Dark: "#0B1416"} // dark text on cyan bar
)

var (
	// titleStyle — primary readout title (bold accent), e.g. a node's own name.
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	// valueStyle — emphasized data value (bold, default fg).
	valueStyle = lipgloss.NewStyle().Bold(true)

	// selectedRowStyle — the cyan selection bar; dark text for contrast on cyan.
	selectedRowStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSelText).Background(colorAccent)

	mutedStyle = lipgloss.NewStyle().Foreground(colorMuted)

	liveBadgeStyle = lipgloss.NewStyle().Bold(true).Foreground(colorLive)

	// tailChipOn/Off are the two pulse phases of the "TAILING" badge. Both render
	// the same text so the pulse changes only intensity, never width.
	tailChipOn  = lipgloss.NewStyle().Bold(true).Foreground(colorSelText).Background(colorLive)
	tailChipOff = lipgloss.NewStyle().Bold(true).Foreground(colorLive)

	footerStyle = lipgloss.NewStyle().Foreground(colorMuted)

	// Diff colors fold into the score palette so the whole UI shares one identity.
	addLineStyle  = lipgloss.NewStyle().Foreground(colorGood)
	delLineStyle  = lipgloss.NewStyle().Foreground(colorBad)
	hunkLineStyle = lipgloss.NewStyle().Foreground(colorAccent)

	modalBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2)

	yesStyle = lipgloss.NewStyle().Bold(true).Foreground(colorGood)
	noStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorBad)
)

// eyebrowStyle renders a section label as a muted, uppercased eyebrow with a
// leading rule bar — the typographic device that gives the detail pane hierarchy.
func eyebrow(label string) string {
	return lipgloss.NewStyle().Foreground(colorAccent).Render("▌") +
		mutedStyle.Bold(true).Render(" "+strings.ToUpper(label))
}

// titledPanel wraps body in a rounded panel whose top border carries a left title
// (and optional right-aligned info), e.g. ╭─ runs ───────────── 2 runnable ─╮.
// When active, the border and title use the accent color; otherwise the border is
// dim and the title muted. innerWidth is the content width (excludes the borders).
func titledPanel(title, info, body string, innerWidth int, active bool) string {
	if innerWidth < 1 {
		innerWidth = 1
	}
	border := colorBorder
	titleStyled := mutedStyle.Render(title)
	if active {
		border = colorAccent
		titleStyled = lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(title)
	}
	bs := lipgloss.NewStyle().Foreground(border)

	// Body box with the top edge disabled; we draw a titled top border ourselves.
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		BorderTop(false).
		Width(innerWidth).
		Render(body)

	// Top border line, total width innerWidth+2 to match the box.
	// Layout: ╭─ <title> <fill ─…> [ <info> ] ─╮
	infoSeg := ""
	if info != "" {
		infoSeg = " " + info + " "
	}
	fill := innerWidth - 4 - lipgloss.Width(title) - lipgloss.Width(infoSeg)
	if fill < 0 { // too narrow — drop the info, then clamp
		infoSeg = ""
		fill = innerWidth - 4 - lipgloss.Width(title)
	}
	if fill < 0 {
		fill = 0
	}
	top := bs.Render("╭─ ") + titleStyled + bs.Render(" "+strings.Repeat("─", fill))
	if infoSeg != "" {
		top += bs.Render(" ") + mutedStyle.Render(info) + bs.Render(" ")
	}
	top += bs.Render("─╮")

	return top + "\n" + box
}

// spinnerFrames is a braille spinner cycle for live activity.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// sparkBlocks are the eight rising block glyphs used by the trajectory sparkline.
var sparkBlocks = []rune("▁▂▃▄▅▆▇█")

// sparkline maps a series of 0..1 scores to block glyphs over the series' own
// min→max range, so small score movements are still visible. An interior missing
// entry (score < 0) renders as a dim middle dot; trailing missing entries (e.g. an
// in-progress or interrupted iteration) are dropped so there's no dangling dot.
// When the scores have no spread (a single point or a flat line) they're scaled by
// absolute value instead, so a lone high score still reads tall. kept[i] toggles
// score-color vs dim.
func sparkline(scores []float64, kept []bool) string {
	// Drop trailing unscored points so an in-progress/interrupted run doesn't
	// leave a dangling dot; the sparkline simply grows as iterations complete.
	for len(scores) > 0 && scores[len(scores)-1] < 0 {
		scores = scores[:len(scores)-1]
	}
	min, max := 1.0, 0.0
	any := false
	for _, s := range scores {
		if s < 0 {
			continue
		}
		any = true
		if s < min {
			min = s
		}
		if s > max {
			max = s
		}
	}
	var b strings.Builder
	for i, s := range scores {
		if s < 0 {
			b.WriteString(mutedStyle.Render("·"))
			continue
		}
		level := 0
		if any && max > min {
			level = int((s - min) / (max - min) * float64(len(sparkBlocks)-1))
		} else {
			// No spread (single point or flat line): scale by absolute 0..1 value.
			level = int(s * float64(len(sparkBlocks)-1))
		}
		if level < 0 {
			level = 0
		}
		if level >= len(sparkBlocks) {
			level = len(sparkBlocks) - 1
		}
		glyph := string(sparkBlocks[level])
		if i < len(kept) && !kept[i] {
			b.WriteString(mutedStyle.Render(glyph)) // reverted = ghost rung
		} else {
			b.WriteString(scoreStyle(s).Render(glyph))
		}
	}
	return b.String()
}

// scoreStyle returns a foreground style colored for a 0..1 score.
func scoreStyle(s float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(scoreColor(s))
}

// scoreColor returns a color for a 0..1 score.
func scoreColor(s float64) lipgloss.TerminalColor {
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
