package style

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestPanelPutsTitleOnTheTopBorder(t *testing.T) {
	r := lipgloss.NewRenderer(io.Discard)
	got := panel(r, "Variables", "API_URL  shared", 40)
	first := strings.Split(got, "\n")[0]
	if !strings.Contains(first, "Variables") {
		t.Errorf("title not on the top border: %q", first)
	}
	if lipgloss.Width(first) != 40 {
		t.Errorf("top border is %d columns, want 40: %q", lipgloss.Width(first), first)
	}
}
