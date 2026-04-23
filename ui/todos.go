package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"claudster/store"
	"claudster/tmux"
)

func resolvedStatus(m Model, t store.Todo) string {
	if t.SessionName != "" && tmux.SessionExists(t.SessionName) {
		if m.monitor.Get(t.SessionName).Status == tmux.StatusWorking {
			return store.StatusInProgress
		}
	}
	if t.Status == "" {
		return store.StatusUnstarted
	}
	return t.Status
}

func cycleStatus(current string) string {
	switch current {
	case store.StatusUnstarted, "":
		return store.StatusInProgress
	case store.StatusInProgress:
		return store.StatusDone
	default:
		return store.StatusUnstarted
	}
}

func statusIcon(m Model, t store.Todo) string {
	switch resolvedStatus(m, t) {
	case store.StatusInProgress:
		if t.SessionName != "" && tmux.SessionExists(t.SessionName) &&
			m.monitor.Get(t.SessionName).Status == tmux.StatusWorking {
			return WorkingBadge.Render(spinner[m.spinFrame])
		}
		return WorkingBadge.Render("⟳")
	case store.StatusDone:
		return DoneBadge.Render("✓")
	default:
		return MutedItem.Render("○")
	}
}

// visibleTodos returns all todos sorted: in_progress → unstarted → done.
func visibleTodos(todos []store.Todo, _ bool) []store.Todo {
	out := make([]store.Todo, len(todos))
	copy(out, todos)
	statusOrder := map[string]int{
		store.StatusInProgress: 0,
		store.StatusUnstarted:  1,
		"":                     1,
		store.StatusDone:       2,
	}
	sort.SliceStable(out, func(i, j int) bool {
		return statusOrder[out[i].Status] < statusOrder[out[j].Status]
	})
	return out
}

// groupTodos groups by status for sidebar display.
func groupTodos(todos []store.Todo) map[string][]store.Todo {
	m := make(map[string][]store.Todo)
	for _, t := range todos {
		key := statusGroupLabel(t.Status)
		m[key] = append(m[key], t)
	}
	return m
}

func todoGroupOrder(_ []store.Todo) []string {
	return []string{"In Progress", "Unstarted", "Done"}
}

func statusGroupLabel(status string) string {
	switch status {
	case store.StatusInProgress:
		return "In Progress"
	case store.StatusDone:
		return "Done"
	default:
		return "Unstarted"
	}
}

func todoGroup(t store.Todo) string {
	if t.JiraProject != "" {
		return t.JiraProject
	}
	return "Manual"
}

func renderTodosOverview(m Model, w int) string {
	todos := m.todos.Todos
	var nProgress, nUnstarted, nDone int
	for _, t := range todos {
		switch resolvedStatus(m, t) {
		case store.StatusInProgress:
			nProgress++
		case store.StatusDone:
			nDone++
		default:
			nUnstarted++
		}
	}

	if m.jiraFetching {
		return lipgloss.NewStyle().Padding(0, 1).Render(MutedItem.Render("fetching from Jira…"))
	}
	if m.jiraErr != "" {
		return lipgloss.NewStyle().Padding(0, 1).Render(ErrorStyle.Render("jira error: " + m.jiraErr))
	}
	if len(todos) == 0 {
		return lipgloss.NewStyle().Padding(0, 1).Render(MutedItem.Render("No todos yet — press a to add one"))
	}

	sep := MutedItem.Render("  ·  ")
	parts := []string{
		WorkingBadge.Render(fmt.Sprintf("⟳  %d in progress", nProgress)),
		MutedItem.Render(fmt.Sprintf("○  %d unstarted", nUnstarted)),
		DoneBadge.Render(fmt.Sprintf("✓  %d done", nDone)),
	}
	return lipgloss.NewStyle().Padding(0, 1).Render(strings.Join(parts, sep))
}

func renderTodosOverviewPanel(m Model, w, h int) string {
	todos := m.todos.Todos
	var lines []string

	// Stats bar
	lines = append(lines, renderTodosOverview(m, w))
	lines = append(lines, MutedItem.Render(strings.Repeat("─", w-2)))
	lines = append(lines, "")

	// Debug: Jira config status
	jiraURL := m.config.Jira.URL
	if jiraURL == "" {
		lines = append(lines, MutedItem.PaddingLeft(1).Render("jira: not configured"))
	} else if m.jiraFetching {
		lines = append(lines, WorkingBadge.PaddingLeft(1).Render("jira: fetching…"))
	} else if m.jiraErr != "" {
		lines = append(lines, ErrorStyle.PaddingLeft(1).Render("jira error: "+m.jiraErr))
	} else {
		nJira := 0
		for _, t := range todos {
			if t.Source == "jira" {
				nJira++
			}
		}
		lines = append(lines, DoneBadge.PaddingLeft(1).Render(
			fmt.Sprintf("jira: synced — %d shown / %d total from API", nJira, m.jiraTotal),
		))
	}
	lines = append(lines, "")

	// In-progress items
	var inProgress []store.Todo
	for _, t := range todos {
		if resolvedStatus(m, t) == store.StatusInProgress {
			inProgress = append(inProgress, t)
		}
	}

	if len(inProgress) > 0 {
		lines = append(lines,
			lipgloss.NewStyle().Foreground(ColorSubtle).Bold(true).PaddingLeft(1).Render("In Progress"),
		)
		for _, t := range inProgress {
			icon := statusIcon(m, t)
			title := t.Title
			maxW := w - 6
			if len(title) > maxW && maxW > 3 {
				title = title[:maxW-1] + "…"
			}
			var sub string
			if t.SessionName != "" && tmux.SessionExists(t.SessionName) {
				state := m.monitor.Get(t.SessionName)
				if state.Status == tmux.StatusWorking {
					sub = WorkingBadge.Render("  working")
				} else {
					sub = MutedItem.Render("  idle")
				}
			} else if t.SessionName != "" {
				sub = MutedItem.Render("  stopped")
			}
			lines = append(lines, "  "+icon+" "+WorkingBadge.Bold(false).Render(title)+sub)
		}
		lines = append(lines, "")
	}

	// Unstarted count hint
	var nUnstarted int
	for _, t := range todos {
		if resolvedStatus(m, t) == store.StatusUnstarted {
			nUnstarted++
		}
	}
	if nUnstarted > 0 {
		lines = append(lines,
			lipgloss.NewStyle().Foreground(ColorSubtle).Bold(true).PaddingLeft(1).Render("Unstarted"),
		)
		visibles := visibleTodos(todos, true)
		shown := 0
		for _, t := range visibles {
			if resolvedStatus(m, t) != store.StatusUnstarted {
				continue
			}
			title := t.Title
			maxW := w - 6
			if len(title) > maxW && maxW > 3 {
				title = title[:maxW-1] + "…"
			}
			lines = append(lines, "  "+MutedItem.Render("○")+" "+MutedItem.Render(title))
			shown++
			if shown >= 5 {
				remaining := nUnstarted - shown
				if remaining > 0 {
					lines = append(lines, MutedItem.PaddingLeft(4).Render(fmt.Sprintf("… %d more", remaining)))
				}
				break
			}
		}
		lines = append(lines, "")
	}

	if len(todos) == 0 {
		lines = append(lines, MutedItem.PaddingLeft(2).Render("No todos yet — press a to add one"))
	} else {
		lines = append(lines, MutedItem.PaddingLeft(2).Render("j/k to browse  ·  enter to start  ·  a to add"))
	}

	return lipgloss.NewStyle().Padding(0, 1).Render(strings.Join(lines, "\n"))
}

func renderTodoDetail(m Model, w, h int) string {
	visibles := visibleTodos(m.todos.Todos, true)

	if m.todoCursor == -1 {
		return renderTodosOverviewPanel(m, w, h)
	}

	overview := renderTodosOverview(m, w)
	overviewH := strings.Count(overview, "\n") + 2
	rule := MutedItem.Render(strings.Repeat("─", w-2))

	if len(visibles) == 0 || m.todoCursor >= len(visibles) {
		return lipgloss.NewStyle().Padding(0, 1).Render(
			strings.Join([]string{overview, rule}, "\n"),
		)
	}

	todo := visibles[m.todoCursor]

	kv := func(k, v string) string {
		return PreviewKey.Render(k+":") + "  " + PreviewValue.Render(v)
	}

	var lines []string
	lines = append(lines, overview, rule, "")

	if todo.JiraKey != "" {
		lines = append(lines, kv("ticket", todo.JiraKey))
		lines = append(lines, kv("project", todoGroup(todo)))
	} else {
		lines = append(lines, kv("source", "manual"))
	}
	if todo.ProjectName != "" {
		lines = append(lines, kv("repo", todo.ProjectName))
	}

	statusStr := resolvedStatus(m, todo)
	var statusDisplay string
	switch statusStr {
	case store.StatusInProgress:
		statusDisplay = WorkingBadge.Render("in progress")
	case store.StatusDone:
		statusDisplay = DoneBadge.Render("done")
	default:
		statusDisplay = MutedItem.Render("unstarted")
	}
	lines = append(lines, PreviewKey.Render("status:")+"  "+statusDisplay)
	if len(todo.Labels) > 0 {
		var rendered []string
		for _, l := range todo.Labels {
			rendered = append(rendered, MutedItem.Render("["+l+"]"))
		}
		lines = append(lines, PreviewKey.Render("labels:")+"  "+strings.Join(rendered, " "))
	}
	lines = append(lines, "")
	lines = append(lines, NormalItem.Bold(true).Render(todo.Title))

	if todo.Description != "" {
		lines = append(lines, "")
		lines = append(lines, PreviewKey.Render("description:"))
		for _, line := range wrapText(todo.Description, w-4) {
			lines = append(lines, MutedItem.PaddingLeft(2).Render(line))
		}
	}

	detailLines := len(lines)

	if todo.SessionName != "" {
		lines = append(lines, "")
		lines = append(lines, MutedItem.Render(strings.Repeat("─", w-4)))
		sessionLabel := "session: " + todo.SessionName
		if tmux.SessionExists(todo.SessionName) {
			state := m.monitor.Get(todo.SessionName)
			icon := sidebarIcon(m, state, true)
			lines = append(lines, icon+"  "+PreviewKey.Render(sessionLabel))
		} else {
			lines = append(lines, MutedItem.Render("○  "+sessionLabel+" (stopped)"))
		}
		lines = append(lines, "")

		previewH := h - detailLines - overviewH - 4
		if previewH > 3 && tmux.SessionExists(todo.SessionName) {
			pane := tmux.CapturePaneOutput(todo.SessionName, previewH)
			if pane != "" {
				rawLines := strings.Split(pane, "\n")
				for i, l := range rawLines {
					rawLines[i] = strings.TrimRight(l, " ")
				}
				if len(rawLines) > previewH {
					rawLines = rawLines[len(rawLines)-previewH:]
				}
				lines = append(lines, rawLines...)
			}
		}
	} else {
		lines = append(lines, "")
		hint := "  enter: start  ·  n: setup  ·  s: cycle status"
		if todo.JiraKey != "" {
			hint += "  ·  o: open in Jira"
		}
		lines = append(lines, MutedItem.Render(hint))
	}

	return lipgloss.NewStyle().Padding(0, 1).Render(strings.Join(lines, "\n"))
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	var lines []string
	var current strings.Builder
	for _, w := range words {
		if current.Len() > 0 && current.Len()+1+len(w) > width {
			lines = append(lines, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(w)
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}
