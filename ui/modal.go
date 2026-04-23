package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"claudster/skills"
)

func renderModal(m Model) string {
	if m.modal.mode == modalConfirmDelete {
		return renderConfirmDelete(m)
	}
	if m.modal.mode == modalConfirmSkillDelete {
		return renderConfirmSkillDelete(m)
	}
	if m.modal.mode == modalHelp {
		return renderHelp(m)
	}
	if m.modal.mode == modalScratchAppend {
		return renderScratchAppend(m)
	}
	if m.modal.mode == modalAddTodo {
		return renderAddTodo(m)
	}
	if m.modal.mode == modalRunTodoAgent {
		return renderRunTodoAgent(m)
	}
	if m.modal.mode == modalSetStatus {
		return renderSetStatus(m)
	}
	if m.modal.step == 1 {
		return renderDangerousConfirm(m)
	}

	var title, fieldLabel, hint string

	switch m.modal.mode {
	case modalNewSkill:
		title = "New Skill"
		fieldLabel = "Skill name:"
		scopeLabel := "Global"
		if m.modal.targetSkillScope != skills.GlobalDir() {
			scopeLabel = filepath.Base(m.modal.targetSkillScope)
		}
		hint = fmt.Sprintf("scope: %s  ·  creates <scope>/<name>/SKILL.md", scopeLabel)

	case modalNewProject:
		title = "New Project"
		fieldLabel = "Group:"
		hint = "tab to autocomplete  ·  template opens in " + resolveEditor(m.config.UI.Editor)

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
			if m.config.UI.GitClient == "github-desktop" {
				hint = "note: GitHub Desktop doesn't run in tmux — use G to open it instead"
			}
		} else {
			title = fmt.Sprintf("New Editor Session — %s", m.modal.targetProject)
			hint = "opens " + resolveEditor(m.config.UI.Editor) + " in " + primaryRepoHint(m)
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
			{"ctrl+d / ctrl+u", "jump 5 rows down / up"},
			{"/", "search sessions"},
			{"space", "expand / collapse group"},
		}},
		{"Sessions", []binding{
			{"enter", "attach or start session"},
			{"n", "new Claude session"},
			{"r", "resume Claude session (picker)"},
			{"c", "scroll output (vim copy mode)"},
			{"T", "new terminal session (persistent)"},
			{"V", "new editor session (persistent)"},
			{"d", "delete session (confirm required)"},
			{"P", "restart Claude session"},
		}},
		{"Quick open", []binding{
			{"v", "open repo in editor"},
			{"t", "open repo in terminal"},
			{"G", "open repo in lazygit"},
			{"s", "quick-add to scratch"},
			{"S", "open full scratch file"},
		}},
		{"Todos", []binding{
			{"tab", "toggle todos panel"},
			{"a", "add manual todo"},
			{"enter", "run agent on todo"},
			{"o", "open Jira issue in browser"},
			{"s", "set status"},
			{"d", "delete todo"},
		}},
		{"Skills", []binding{
			{"a", "new skill (in current scope)"},
			{"enter / v", "edit skill in editor"},
			{"d", "delete skill"},
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
	var title, label, hint string
	if m.sidebarMode == sidebarModeTodos {
		title = "Delete Todo"
		hint = "This will permanently remove the todo."
		visibles := visibleTodos(m.todos.Todos, true)
		if m.todoCursor < len(visibles) {
			label = visibles[m.todoCursor].Title
		}
	} else {
		title = "Delete Session"
		hint = "This will kill the tmux session and remove it from config."
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			label = m.rows[m.cursor].label
		}
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		OverlayTitle.Render(title),
		"",
		HelpDesc.Render("Are you sure you want to delete:"),
		"",
		lipgloss.NewStyle().Foreground(ColorText).Bold(true).PaddingLeft(2).Render(label),
		"",
		HelpDesc.Render(hint),
		"",
		ErrorStyle.Render("y")+HelpSep.Render("  confirm    ")+HelpKey.Render("esc")+HelpSep.Render("  cancel"),
	)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		OverlayStyle.Render(body),
	)
}

func renderConfirmSkillDelete(m Model) string {
	body := lipgloss.JoinVertical(lipgloss.Left,
		OverlayTitle.Render("Delete Skill"),
		"",
		HelpDesc.Render("Are you sure you want to delete:"),
		"",
		lipgloss.NewStyle().Foreground(ColorText).Bold(true).PaddingLeft(2).Render(m.modal.targetSkillName),
		MutedItem.PaddingLeft(2).Render(m.modal.targetSkillDir),
		"",
		HelpDesc.Render("This will permanently remove the skill directory."),
		"",
		ErrorStyle.Render("y")+HelpSep.Render("  confirm    ")+HelpKey.Render("esc")+HelpSep.Render("  cancel"),
	)
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		OverlayStyle.Render(body),
	)
}

func renderScratchAppend(m Model) string {
	body := lipgloss.JoinVertical(lipgloss.Left,
		OverlayTitle.Render("Scratch — "+m.modal.targetProject),
		"",
		HelpDesc.Render("Quick note (appended as a bullet):"),
		InputStyle.Width(46).Render(m.modal.input.View()),
		"",
		HelpDesc.Render("enter  save    S  open full scratch    esc  cancel"),
	)
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		OverlayStyle.Render(body),
	)
}

func renderDangerousConfirm(m Model) string {
	body := lipgloss.JoinVertical(lipgloss.Left,
		OverlayTitle.Render("New Session — "+m.modal.targetProject),
		"",
		PreviewKey.Render("Session name:"),
		NormalItem.PaddingLeft(2).Render(m.modal.pendingName),
		"",
		PreviewKey.Render("Run with --dangerously-skip-permissions?"),
		HelpDesc.Render("Skips permission prompts. Only use if you trust the codebase."),
		"",
		HelpKey.Render("y")+" "+HelpDesc.Render("yes    ")+HelpKey.Render("n / enter")+" "+HelpDesc.Render("no    ")+HelpKey.Render("esc")+" "+HelpDesc.Render("cancel"),
	)
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		OverlayStyle.Render(body),
	)
}

// resolveEditor returns the editor to use, checking (in order):
// config ui.editor → $EDITOR env → auto-detect from PATH.
func resolveEditor(configured string) string {
	if configured != "" {
		return configured
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	for _, e := range []string{"code", "code-insiders", "nano", "vim", "vi"} {
		if _, err := exec.LookPath(e); err == nil {
			return e
		}
	}
	return "vi"
}

// isVSCode reports whether the editor binary is VS Code.
func isVSCode(editor string) bool {
	base := filepath.Base(editor)
	return base == "code" || base == "code-insiders"
}

// wslPath converts an absolute Linux path to a Windows path when running
// under WSL, so VS Code (a Windows app) can open the file correctly.
// On non-WSL systems it returns the path unchanged.
func wslPath(path string) string {
	out, err := exec.Command("wslpath", "-w", path).Output()
	if err != nil {
		return path
	}
	return strings.TrimSpace(string(out))
}

func openURL(url string) {
	// WSL: use cmd.exe start
	if err := exec.Command("cmd.exe", "/c", "start", "", url).Start(); err == nil {
		return
	}
	// Linux fallback
	exec.Command("xdg-open", url).Start()
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

func renderAddTodo(m Model) string {
	var fieldLabel, hint string
	switch m.modal.step {
	case 0:
		fieldLabel = "Title:"
		hint = ""
	case 1:
		fieldLabel = "Description:"
		hint = "optional — enter to skip"
	case 2:
		fieldLabel = "Project:"
		hint = "tab to autocomplete  ·  enter to skip"
	}
	var rows []string
	rows = append(rows, OverlayTitle.Render("Add Todo"), "", PreviewKey.Render(fieldLabel))
	if hint != "" {
		rows = append(rows, HelpDesc.Render(hint))
	}
	rows = append(rows, InputStyle.Width(46).Render(m.modal.input.View()), "", HelpDesc.Render("enter  confirm    esc  cancel"))
	body := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		OverlayStyle.Render(body),
	)
}

func renderRunTodoAgent(m Model) string {
	var title, fieldLabel, hint string
	todo := m.modal.pendingTodo

	todoLabel := ""
	if todo != nil {
		if todo.JiraKey != "" {
			todoLabel = todo.JiraKey + ": " + todo.Title
		} else {
			todoLabel = todo.Title
		}
	}

	switch m.modal.step {
	case 0:
		title = "Run Agent — " + todoLabel
		fieldLabel = "Project:"
		hint = "tab to autocomplete  ·  e.g. work/myapp"
	case 1:
		title = "Run Agent — " + todoLabel
		fieldLabel = "Session name:"
		hint = "starts in " + primaryRepoHint(m)
	case 2:
		return renderDangerousConfirmTodo(m, todoLabel)
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

func renderDangerousConfirmTodo(m Model, todoLabel string) string {
	body := lipgloss.JoinVertical(lipgloss.Left,
		OverlayTitle.Render("Run Agent — "+todoLabel),
		"",
		PreviewKey.Render("Session name:"),
		NormalItem.PaddingLeft(2).Render(m.modal.pendingName),
		"",
		PreviewKey.Render("Run with --dangerously-skip-permissions?"),
		HelpDesc.Render("Skips permission prompts. Only use if you trust the codebase."),
		"",
		HelpKey.Render("y")+" "+HelpDesc.Render("yes    ")+HelpKey.Render("n / enter")+" "+HelpDesc.Render("no    ")+HelpKey.Render("esc")+" "+HelpDesc.Render("cancel"),
	)
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		OverlayStyle.Render(body),
	)
}

func renderSetStatus(m Model) string {
	type option struct{ icon, label string }
	options := []option{
		{MutedItem.Render("○"), "Unstarted"},
		{WorkingBadge.Render("⟳"), "In Progress"},
		{DoneBadge.Render("✓"), "Done"},
	}

	var rows []string
	rows = append(rows, OverlayTitle.Render("Set Status"), "")
	for i, opt := range options {
		if i == m.modal.statusCursor {
			cursor := SelectedItem.Render("▶ ")
			label := SelectedItem.Bold(true).Render(opt.icon + "  " + opt.label)
			rows = append(rows, lipgloss.NewStyle().PaddingLeft(2).Render(cursor+label))
		} else {
			rows = append(rows, MutedItem.PaddingLeft(4).Render(opt.icon+"  "+opt.label))
		}
	}
	rows = append(rows, "", HelpDesc.Render("j/k  move    enter  confirm    esc  cancel"))

	body := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		OverlayStyle.Render(body),
	)
}
