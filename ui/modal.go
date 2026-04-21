package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderModal(m Model) string {
	if m.modal.mode == modalConfirmDelete {
		return renderConfirmDelete(m)
	}
	if m.modal.mode == modalHelp {
		return renderHelp(m)
	}

	var title, fieldLabel, hint string

	switch m.modal.mode {
	case modalNewProject:
		title = "New Project"
		fieldLabel = "Group:"
		hint = "tab to autocomplete  ·  template opens in " + resolveEditor()

	case modalNewSession:
		title = fmt.Sprintf("New Session — %s", m.modal.targetProject)
		fieldLabel = "Session name:"
		hint = "starts in " + primaryRepoHint(m)
		if m.dangerousMode {
			hint += "  " + ErrorStyle.Render("⚠ --dangerously-skip-permissions")
		}

	case modalResumeSession:
		title = fmt.Sprintf("Resume Session — %s", m.modal.targetProject)
		fieldLabel = "Session name:"
		hint = "opens claude --resume picker in " + primaryRepoHint(m)
		if m.dangerousMode {
			hint += "  " + ErrorStyle.Render("⚠ --dangerously-skip-permissions")
		}

	case modalNewEditorSession:
		if m.modal.targetKind == "lazygit" {
			title = fmt.Sprintf("New Lazygit Session — %s", m.modal.targetProject)
			hint = "opens lazygit in " + primaryRepoHint(m)
		} else {
			title = fmt.Sprintf("New Editor Session — %s", m.modal.targetProject)
			hint = "opens " + resolveEditor() + " in " + primaryRepoHint(m)
		}
		fieldLabel = "Session name:"
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		OverlayTitle.Render(title),
		"",
		PreviewKey.Render(fieldLabel),
		HelpDesc.Render(hint),
		InputStyle.Width(46).Render(m.modal.input.View()),
		"",
		HelpDesc.Render("enter  confirm    esc  cancel"),
	)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		OverlayStyle.Render(body),
	)
}

func renderHelp(m Model) string {
	type binding struct{ key, desc string }
	sections := []struct {
		title    string
		bindings []binding
	}{
		{"Navigate", []binding{
			{"j / ↓", "move down"},
			{"k / ↑", "move up"},
			{"space", "expand / collapse project"},
		}},
		{"Sessions", []binding{
			{"enter", "attach or start session"},
			{"n", "new Claude session"},
			{"r", "resume Claude session (picker)"},
			{"T", "new terminal session (persistent)"},
			{"V", "new editor session (persistent)"},
			{"d", "delete session (confirm required)"},
			{"P", "restart Claude session"},
		}},
		{"Quick open", []binding{
			{"v", "open repo in editor"},
			{"t", "open repo in terminal"},
			{"G", "open repo in lazygit"},
		}},
		{"Project / config", []binding{
			{"N", "new project"},
			{"e", "edit config file"},
		}},
		{"UI", []binding{
			{"[ / ]", "resize sidebar"},
			{"p", "toggle --dangerously-skip-permissions"},
			{"?", "this help page"},
			{"q / ctrl+q", "quit"},
		}},
	}

	var col1, col2 []string
	col1 = append(col1, OverlayTitle.Render("Keybindings"), "")
	col2 = append(col2, "", "")

	for _, sec := range sections {
		col1 = append(col1, PreviewKey.Render(sec.title))
		col2 = append(col2, "")
		for _, b := range sec.bindings {
			col1 = append(col1, HelpKey.Render("  "+b.key))
			col2 = append(col2, HelpDesc.Render(b.desc))
		}
		col1 = append(col1, "")
		col2 = append(col2, "")
	}

	// Pad both columns to equal length
	for len(col1) < len(col2) {
		col1 = append(col1, "")
	}
	for len(col2) < len(col1) {
		col2 = append(col2, "")
	}

	keyW := 24
	var rows []string
	for i := range col1 {
		keyCell := lipgloss.NewStyle().Width(keyW).Render(col1[i])
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, keyCell, col2[i]))
	}

	rows = append(rows, HelpDesc.Render("esc  close"))

	body := strings.Join(rows, "\n")
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		OverlayStyle.Render(body),
	)
}

func renderConfirmDelete(m Model) string {
	sessionName := ""
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		sessionName = m.rows[m.cursor].label
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		OverlayTitle.Render("Delete Session"),
		"",
		HelpDesc.Render("Are you sure you want to delete:"),
		"",
		lipgloss.NewStyle().Foreground(ColorText).Bold(true).PaddingLeft(2).Render(sessionName),
		"",
		HelpDesc.Render("This will kill the tmux session and remove it from config."),
		"",
		ErrorStyle.Render("y")+HelpSep.Render("  confirm    ")+HelpKey.Render("esc")+HelpSep.Render("  cancel"),
	)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		OverlayStyle.Render(body),
	)
}

func resolveEditor() string {
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	for _, e := range []string{"nano", "vim", "vi"} {
		if _, err := exec.LookPath(e); err == nil {
			return e
		}
	}
	return "vi"
}

func primaryRepoHint(m Model) string {
	for _, g := range m.config.Groups {
		if g.Name == m.modal.targetGroup {
			for _, p := range g.Projects {
				if p.Name == m.modal.targetProject {
					return p.PrimaryRepo()
				}
			}
		}
	}
	return "primary repo"
}
