package ui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

func metaKey() string {
	if runtime.GOOS == "darwin" {
		return "opt"
	}
	return "alt"
}

const toastTTL = 10 * time.Second
const toastMaxCount = 5

// toastInnerW is the lipgloss Width() of the toast box (inner content + padding).
// Padding(0,1) eats 2 chars, so usable content width = toastInnerW - 2 = 20.
const toastInnerW = 22

// toastOuterW is the full visual width including rounded border chars.
const toastOuterW = toastInnerW + 2

type toast struct {
	sessionName string
	createdAt   time.Time
}

var toastStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorSuccess).
	Padding(0, 1).
	Width(toastInnerW)

// addToast appends a toast for sessionName. Ignores duplicates. Drops the
// oldest when the stack is full.
func (m *Model) addToast(sessionName string) {
	for _, t := range m.toasts {
		if t.sessionName == sessionName {
			return
		}
	}
	if len(m.toasts) >= toastMaxCount {
		m.toasts = m.toasts[1:]
	}
	m.toasts = append(m.toasts, toast{sessionName: sessionName, createdAt: time.Now()})
	prev := m.tmuxBoundCount
	m.tmuxBoundCount = len(m.toasts)
	snapshot := append([]toast{}, m.toasts...)
	go func() {
		tmuxDisplayToast(sessionName)
		tmuxSyncBindings(snapshot, prev)
	}()
}

// dismissToast removes the toast at idx.
func (m *Model) dismissToast(idx int) {
	if idx < 0 || idx >= len(m.toasts) {
		return
	}
	m.toasts = append(m.toasts[:idx], m.toasts[idx+1:]...)
	prev := m.tmuxBoundCount
	m.tmuxBoundCount = len(m.toasts)
	go tmuxSyncBindings(append([]toast{}, m.toasts...), prev)
}

// tickToasts removes expired toasts.
func (m *Model) tickToasts() {
	now := time.Now()
	var kept []toast
	for _, t := range m.toasts {
		if now.Sub(t.createdAt) < toastTTL {
			kept = append(kept, t)
		}
	}
	if len(kept) == len(m.toasts) {
		return
	}
	prev := m.tmuxBoundCount
	m.toasts = kept
	m.tmuxBoundCount = len(m.toasts)
	go tmuxSyncBindings(append([]toast{}, kept...), prev)
}

// tmuxDisplayToast sends a display-message notification to every attached tmux
// client so the user sees it regardless of which session they're in.
func tmuxDisplayToast(name string) {
	if os.Getenv("TMUX") == "" {
		return
	}
	msg := fmt.Sprintf(" ✓ %s done  ·  prefix+g to jump ", name)
	out, err := exec.Command("tmux", "list-clients", "-F", "#{client_name}").Output()
	if err != nil {
		return
	}
	for _, client := range strings.Fields(string(out)) {
		exec.Command("tmux", "display-message", "-c", client, "-d", "8000", msg).Run()
	}
}

// tmuxSyncBindings binds prefix+g to a display-menu listing all done sessions,
// so the user can jump from anywhere in tmux without Alt key conflicts.
// When there are no more toasts, the binding is removed.
func tmuxSyncBindings(toasts []toast, prevCount int) {
	if os.Getenv("TMUX") == "" {
		return
	}
	if len(toasts) == 0 {
		exec.Command("tmux", "unbind-key", "g").Run()
		return
	}
	// Build: tmux bind-key g display-menu -T " ✓ Done " <label> <key> <cmd> ...
	args := []string{"bind-key", "g", "display-menu", "-T", " ✓ Done sessions "}
	for i, t := range toasts {
		args = append(args,
			t.sessionName,
			fmt.Sprintf("%d", i+1),
			fmt.Sprintf("switch-client -t '%s'", t.sessionName),
		)
	}
	exec.Command("tmux", args...).Run()
}

// jumpToToast moves the sidebar cursor to the session for toast idx, dismisses
// the toast, and returns the session name so the caller can switch to it.
func (m *Model) jumpToToast(idx int) string {
	if idx < 0 || idx >= len(m.toasts) {
		return ""
	}
	name := m.toasts[idx].sessionName
	m.dismissToast(idx)
	for i, r := range m.rows {
		if r.typ == rowTypeSession && r.label == name {
			m.cursor = i
			break
		}
	}
	return name
}

func renderToastBox(t toast, idx int) string {
	const contentW = toastInnerW - 2 // subtract padding

	name := t.sessionName
	// Truncate name to fit: "✓ " takes 2 chars
	maxNameW := contentW - 2
	if runewidth.StringWidth(name) > maxNameW {
		truncated := []rune(name)
		w := 0
		cut := 0
		for i, r := range truncated {
			rw := runewidth.RuneWidth(r)
			if w+rw > maxNameW-1 {
				cut = i
				break
			}
			w += rw
		}
		if cut > 0 {
			name = string(truncated[:cut]) + "…"
		}
	}

	age := time.Since(t.createdAt)
	var ageStr string
	if age < time.Minute {
		ageStr = "just now"
	} else {
		ageStr = relTime(t.createdAt)
	}

	line1 := DoneBadge.Render("✓") + " " + NormalItem.Render(name)
	keyHint := HelpKey.Render(fmt.Sprintf("%d", idx+1)) + HelpDesc.Render(" · prefix+g from tmux")
	line2 := TimestampStyle.Render(ageStr) + HelpSep.Render("  ·  ") + keyHint

	return toastStyle.Render(lipgloss.JoinVertical(lipgloss.Left, line1, line2))
}

func renderAllToasts(m Model) string {
	if len(m.toasts) == 0 {
		return ""
	}
	boxes := make([]string, len(m.toasts))
	for i, t := range m.toasts {
		boxes[i] = renderToastBox(t, i)
	}
	return lipgloss.JoinVertical(lipgloss.Left, boxes...)
}

// overlayStrings places the multi-line string `top` over `base` starting at
// visual position (x, y) (0-indexed). Lines in `top` that fall outside `base`
// are silently ignored.
func overlayStrings(base, top string, x, y int) string {
	if top == "" {
		return base
	}
	baseLines := strings.Split(base, "\n")
	topLines := strings.Split(top, "\n")
	for i, tl := range topLines {
		lineIdx := y + i
		if lineIdx < 0 || lineIdx >= len(baseLines) {
			continue
		}
		baseLines[lineIdx] = replaceAtVisualCol(baseLines[lineIdx], tl, x)
	}
	return strings.Join(baseLines, "\n")
}

// replaceAtVisualCol replaces visual columns [x, x+width(overlay)) in `base`
// with `overlay`. ANSI escape sequences in `base` are preserved outside the
// replaced region.
func replaceAtVisualCol(base, overlay string, x int) string {
	overlayW := lipgloss.Width(overlay)
	endCol := x + overlayW

	var result strings.Builder
	col := 0
	i := 0
	bs := []byte(base)

	// Phase 1: emit base as-is up to visual column x.
	for i < len(bs) && col < x {
		if bs[i] == '\x1b' {
			j := i
			i = skipAnsiSeq(bs, i)
			result.Write(bs[j:i])
			continue
		}
		r, size := utf8.DecodeRune(bs[i:])
		rw := runewidth.RuneWidth(r)
		if col+rw > x {
			// Wide rune straddles boundary — pad with spaces.
			for col < x {
				result.WriteByte(' ')
				col++
			}
			i += size
			break
		}
		result.WriteRune(r)
		col += rw
		i += size
	}
	// Pad if base was shorter than x.
	for col < x {
		result.WriteByte(' ')
		col++
	}

	// Phase 2: emit overlay and reset ANSI state afterward.
	result.WriteString(overlay)
	result.WriteString("\x1b[0m")

	// Phase 3: skip base characters that fall under the overlay region.
	for i < len(bs) && col < endCol {
		if bs[i] == '\x1b' {
			i = skipAnsiSeq(bs, i)
			continue
		}
		r, size := utf8.DecodeRune(bs[i:])
		col += runewidth.RuneWidth(r)
		i += size
	}

	// Phase 4: emit the rest of base unchanged.
	result.Write(bs[i:])

	return result.String()
}

// skipAnsiSeq advances past a single ANSI/VT escape sequence starting at i
// (where bs[i] == '\x1b') and returns the new index.
func skipAnsiSeq(bs []byte, i int) int {
	i++ // skip ESC
	if i >= len(bs) {
		return i
	}
	switch bs[i] {
	case '[': // CSI sequence
		i++
		for i < len(bs) && (bs[i] < 0x40 || bs[i] > 0x7E) {
			i++
		}
		if i < len(bs) {
			i++ // consume final byte
		}
	case ']': // OSC sequence — terminated by BEL or ST
		i++
		for i < len(bs) {
			if bs[i] == '\x07' {
				i++
				break
			}
			if bs[i] == '\x1b' && i+1 < len(bs) && bs[i+1] == '\\' {
				i += 2
				break
			}
			i++
		}
	default:
		i++ // 2-byte escape (e.g. ESC M)
	}
	return i
}
