package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"claudster/store"
	"claudster/tmux"
)

func renderSidebar(m Model) string {
	innerH := m.height - 3

	title := PanelTitle.Render("claudster") + "  " + MutedItem.Render(Version)
	if m.dangerousMode {
		title += "  " + ErrorStyle.Render("⚠ bypass")
	}
	header := lipgloss.NewStyle().Padding(0, 1).Render(title)

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
		prevWasGroup := false
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
				lines = append(lines, lipgloss.NewStyle().
					Foreground(ColorSubtle).
					Bold(true).
					PaddingLeft(1).
					Render(row.label))
				prevWasGroup = true

			case rowTypeProject:
				_ = prevWasGroup
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
						if tmux.SessionExists(s.Name) {
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
				running := tmux.SessionExists(row.label)
				sess := m.config.Groups[row.groupIdx].Projects[row.projectIdx].Sessions[row.sessionIdx]
				lines = append(lines, renderSidebarSession(m, i, row, sess, state, running))
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
		return WorkingBadge.Render(spinner[m.spinFrame])
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
