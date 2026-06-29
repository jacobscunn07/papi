package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"papi/internal/runs"
)

func TestTailBadge(t *testing.T) {
	run := runs.Run{Timestamp: "1782500303124", Iterations: []runs.Iteration{{Index: 0, Score: 0.8}}}
	base := &model{
		mode: modeBrowse, skillName: "terraform-author", width: 96, height: 24,
		live: &run, liveStatus: map[string]string{}, streams: map[string]*strings.Builder{},
	}
	base.selectedKey = runKey(run.Timestamp)

	// Following a live run → badge present.
	base.liveActive, base.follow = true, true
	on := base.headerRibbon()
	if !strings.Contains(on, "TAILING") {
		t.Fatalf("expected TAILING badge when following a live run:\n%s", on)
	}

	// Not following → no badge.
	base.follow = false
	if off := base.headerRibbon(); strings.Contains(off, "TAILING") {
		t.Fatalf("did not expect TAILING badge when not following:\n%s", off)
	}

	// Not live → no badge even if follow is set.
	base.follow, base.liveActive = true, false
	if off := base.headerRibbon(); strings.Contains(off, "TAILING") {
		t.Fatalf("did not expect TAILING badge when run is not live:\n%s", off)
	}

	// Pulse phases keep identical width (no reflow).
	base.liveActive, base.follow = true, true
	base.spinnerIdx = 0 // on phase
	w0 := lipgloss.Width(firstLine(base.headerRibbon()))
	base.spinnerIdx = 5 // off phase
	w1 := lipgloss.Width(firstLine(base.headerRibbon()))
	if w0 != w1 {
		t.Fatalf("pulse changed header width: %d vs %d", w0, w1)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
