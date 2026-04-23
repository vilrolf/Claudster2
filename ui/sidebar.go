package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"claudster/store"
	"claudster/tmux"
)

func renderSidebar(m Model) string {
	if m.sidebarMode == sidebarModeTodos {
		return renderTodosSidebar(m)
	}
	return renderSessionsSidebar(m)
}

func renderSessionsSidebar(m Model) string {
	innerH := m.height - 3

	var header string
	if m.searchMode {
		searchLine := HelpKey.Render("/") + " " + m.searchStr + "█"
		header = lipgloss.NewStyle().Padding(0, 1).Render(searchLine)
	} else {
		title := PanelTitle.Render("claudster") + "  " + MutedItem.Render(Version)
		if m.dangerousMode {
			title += "  " + ErrorStyle.Render("⚠ bypass")
		}
		modeTab := MutedItem.Render("sessions") + MutedItem.Render("  ·  ") + HelpDesc.Render("tab→todos")
		header = lipgloss.NewStyle().Padding(0, 1).Render(title + "\n" + modeTab)
	}

	var lines []string

	if m.configErr != "" {
		lines = append(lines,
			ErrorStyle.PaddingLeft(1).Render("config error"),
			"",
			MutedItem.PaddingLeft(2).Render(m.configErr),
			"",
			MutedItem.PaddingLeft(2).Render("press e to fix"),
		)
	} else if len(m.rows) == 0 {
		lines = append(lines,
			MutedItem.PaddingLeft(2).Render("no projects yet"),
			"",
			MutedItem.PaddingLeft(2).Render("press N to add one"),
		)
	} else {
		for i, row := range m.rows {
			switch row.typ {
			case rowTypeOverview:
				if i == m.cursor {
					lines = append(lines, SelectedItem.PaddingLeft(1).Render("⊞  overview"))
				} else {
					lines = append(lines, NormalItem.PaddingLeft(1).Render("⊞  overview"))
				}
				lines = append(lines, "") // spacer

			case rowTypeGroup:
				if i > 0 {
					lines = append(lines, "")
				}
				collapsed := m.groupCollapsed[row.groupIdx]
				arrow := "▼"
				if collapsed {
					arrow = "▶"
				}
				label := arrow + " " + row.label
				if i == m.cursor {
					lines = append(lines, SelectedItem.Bold(true).PaddingLeft(1).Render(label))
				} else {
					lines = append(lines, lipgloss.NewStyle().
						Foreground(ColorSubtle).
						Bold(true).
						PaddingLeft(1).
						Render(label))
				}

			case rowTypeProject:
				key := expandKey(row.groupIdx, row.projectIdx)
				arrow := "▶"
				if m.expanded[key] {
					arrow = "▼"
				}
				proj := m.config.Groups[row.groupIdx].Projects[row.projectIdx]
				badge := ""
				if !m.expanded[key] && len(proj.Sessions) > 0 {
					running := 0
					for _, s := range proj.Sessions {
						if m.monitor.Exists(s.Name) {
							running++
						}
					}
					badge = MutedItem.Render(fmt.Sprintf("  %d/%d", running, len(proj.Sessions)))
				}

				label := arrow + " " + row.label
				if i == m.cursor {
					lines = append(lines, SelectedItem.PaddingLeft(2).Render(label)+badge)
				} else {
					lines = append(lines, NormalItem.PaddingLeft(2).Render(label)+badge)
				}

			case rowTypeSession:
				state := m.monitor.Get(row.label)
				running := m.monitor.Exists(row.label)
				sess := m.config.Groups[row.groupIdx].Projects[row.projectIdx].Sessions[row.sessionIdx]
				lines = append(lines, renderSidebarSession(m, i, row, sess, state, running))

			case rowTypeSkillsHeader:
				lines = append(lines, "")
				lines = append(lines, lipgloss.NewStyle().
					Foreground(ColorSubtle).
					Bold(true).
					PaddingLeft(1).
					Render("── "+row.label+" ──"))

			case rowTypeSkillScope:
				label := "  " + row.label
				if i == m.cursor {
					lines = append(lines, SelectedItem.PaddingLeft(1).Render(label))
				} else {
					lines = append(lines, NormalItem.PaddingLeft(1).Render(label))
				}

			case rowTypeSkill:
				label := "✦ " + row.label
				if i == m.cursor {
					lines = append(lines, SelectedItem.PaddingLeft(3).Render(label))
				} else {
					lines = append(lines, MutedItem.PaddingLeft(3).Render(label))
				}
			}
		}
	}

	content := strings.Join(lines, "\n")
	// Clip to innerH so the sidebar border never overflows its allocated height.
	body := clipLines(lipgloss.JoinVertical(lipgloss.Left, header, content), innerH)

	return ActiveBorder.
		Width(m.sidebarW).
		Height(innerH).
		Render(body)
}

func renderTodosSidebar(m Model) string {
	innerH := m.height - 3

	title := PanelTitle.Render("claudster") + "  " + MutedItem.Render(Version)
	modeTab := HelpDesc.Render("tab→sessions") + MutedItem.Render("  ·  ") + PanelTitle.Render("todos")
	header := lipgloss.NewStyle().Padding(0, 1).Render(title + "\n" + modeTab)

	visibles := visibleTodos(m.todos.Todos, true)
	groups := groupTodos(visibles)
	order := todoGroupOrder(visibles)

	var lines []string
	cursor := 0

	// Overview row (always present)
	if m.todoCursor == -1 {
		lines = append(lines, SelectedItem.PaddingLeft(1).Render("⊞  overview"))
	} else {
		lines = append(lines, NormalItem.PaddingLeft(1).Render("⊞  overview"))
	}
	lines = append(lines, "")

	if len(visibles) == 0 {
		lines = append(lines,
			MutedItem.PaddingLeft(2).Render("no todos yet"),
			"",
			MutedItem.PaddingLeft(2).Render("a: add  ·  configure jira"),
		)
	} else {
		for _, grp := range order {
			items := groups[grp]
			if len(items) == 0 {
				continue
			}
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, lipgloss.NewStyle().
				Foreground(ColorSubtle).
				Bold(true).
				PaddingLeft(1).
				Render(grp))

			for _, t := range items {
				selected := cursor == m.todoCursor
				icon := statusIcon(m, t)
				var label string
				if t.JiraKey != "" {
					label = t.JiraKey
				} else {
					label = "note"
				}
				maxTitle := m.sidebarW - lipgloss.Width(icon) - lipgloss.Width(label) - 6
				title := t.Title
				if len(title) > maxTitle && maxTitle > 3 {
					title = title[:maxTitle-1] + "…"
				}
				var line string
				if selected {
					line = "  " + icon + " " + SelectedItem.Render(label) + "  " + SelectedItem.Render(title)
				} else {
					line = "  " + icon + " " + MutedItem.Render(label) + "  " + MutedItem.Render(title)
				}
				lines = append(lines, line)
				cursor++
			}
		}
	}

	content := strings.Join(lines, "\n")
	body := clipLines(lipgloss.JoinVertical(lipgloss.Left, header, content), innerH)

	return ActiveBorder.
		Width(m.sidebarW).
		Height(innerH).
		Render(body)
}

func renderSidebarSession(m Model, i int, row sidebarRow, sess store.Session, state tmux.State, running bool) string {
	var icon, badge string
	if sess.IsToolSession() {
		icon = toolIcon(sess.Kind, running)
		badge = toolBadge(sess.Kind, running)
	} else {
		icon = sidebarIcon(m, state, running)
		badge = sidebarBadge(state, running)
	}

	if i == m.cursor {
		// Style label separately from icon so the icon keeps its own
		// color (amber spinner, green checkmark, etc.) while the label
		// gets the purple/bold selection highlight.
		label := SelectedItem.Render(row.label)
		return lipgloss.NewStyle().PaddingLeft(4).Render(icon+" "+label) + badge
	}

	return NormalItem.PaddingLeft(4).Render(icon+" "+row.label) + badge
}

func toolIcon(kind string, running bool) string {
	if !running {
		return MutedItem.Render("○")
	}
	switch kind {
	case "lazygit":
		return lipgloss.NewStyle().Foreground(ColorSubtle).Render("⎇")
	case "terminal":
		return lipgloss.NewStyle().Foreground(ColorSubtle).Render("$")
	default: // editor
		return lipgloss.NewStyle().Foreground(ColorSubtle).Render("✎")
	}
}

func toolBadge(kind string, running bool) string {
	if !running {
		return MutedItem.Render("  stopped")
	}
	switch kind {
	case "lazygit":
		return MutedItem.Render("  lazygit")
	case "terminal":
		return MutedItem.Render("  terminal")
	default:
		return MutedItem.Render("  editor")
	}
}

func sidebarIcon(m Model, state tmux.State, running bool) string {
	if !running {
		return MutedItem.Render("○")
	}
	switch state.Status {
	case tmux.StatusWorking:
		color := workingPalette[m.spinFrame%len(workingPalette)]
		return lipgloss.NewStyle().Foreground(color).Render(spinner[m.spinFrame])
	case tmux.StatusDone:
		return DoneBadge.Render("✓")
	default:
		return MutedItem.Render("─")
	}
}

func sidebarBadge(state tmux.State, running bool) string {
	if !running {
		return MutedItem.Render("  stopped")
	}
	switch state.Status {
	case tmux.StatusWorking:
		return WorkingBadge.Render("  working")
	case tmux.StatusDone:
		if state.FinishedAt != nil {
			return DoneBadge.Render("  done") + TimestampStyle.Render(" "+relTime(*state.FinishedAt))
		}
		return DoneBadge.Render("  done")
	default:
		return ""
	}
}
