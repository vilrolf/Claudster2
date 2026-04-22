# Claudster — AI Assistant Context

## What this is

Claudster is a terminal UI (TUI) session manager for Claude Code. It lets you organize, launch, and monitor multiple Claude sessions across projects from a single interface. All Claude sessions run inside tmux panes.

This is a fork of [jonagull/Claudster](https://github.com/jonagull/Claudster) maintained by vilrolf, extended with a Todo/task management layer.

---

## Tech Stack

- **Language:** Go 1.26+
- **TUI framework:** [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Lipgloss](https://github.com/charmbracelet/lipgloss)
- **Process management:** tmux
- **Config/persistence:** YAML (`~/.claudster.yaml`, `~/.claudster-todos.yaml`)

---

## Data Model

```
Group
  └── Project   (defines scope of work — owns repos)
        └── Session  (a Claude process running in tmux)
```

**Todo layer (added in this fork):**
```
TodoList (sourced from Jira project or "manual")
  └── Todo
        └── Session  (optional — spawned from the todo)
```

---

## Key Files

```
main.go                  # entry point
store/store.go           # reads/writes ~/.claudster.yaml
store/todos.go           # reads/writes ~/.claudster-todos.yaml (todo persistence)
tmux/session.go          # create/kill/list tmux sessions
tmux/monitor.go          # polls pane output, detects activity
metrics/metrics.go       # tracks Claude API usage stats
ui/model.go              # Bubble Tea event loop + top-level state
ui/dashboard.go          # overview panel (session cards, metrics)
ui/sidebar.go            # collapsible group/project/session tree
ui/modal.go              # dialogs: new session, new project, new todo, run-agent
ui/todos.go              # todo list view
ui/styles.go             # lipgloss color/style definitions
```

---

## Build & Run

```bash
make build        # build for current platform → ./claudster
make install      # build + install to /usr/local/bin
make release      # cross-compile all platforms → dist/
```

Run inside tmux:
```bash
tmux new-session -s claudster -d 'claudster' && tmux attach -t claudster
```

---

## Config Files

`~/.claudster.yaml` — projects, groups, sessions, UI preferences
`~/.claudster-todos.yaml` — todos (Jira-synced + manual), separate so it can be synced independently

---

## Keybindings (existing)

| Key | Action |
|-----|--------|
| `n` | New session |
| `N` | New project |
| `d` | Delete session |
| `t` | Open terminal popup |
| `G` | Open git client |
| `e` | Edit config |
| `?` | Help |
| `q` | Quit |

**Todo view additions:**
| Key | Action |
|-----|--------|
| `T` | Toggle todos view |
| `a` | Add manual todo |
| `enter` | Run agent on selected todo |
| `D` | Delete todo |

---

## Jira Integration

Todos are fetched from Jira on startup using the Atlassian MCP. Configured in `~/.claudster.yaml` under `jira:`. Issues assigned to the current user are pulled in and grouped by Jira project.

```yaml
jira:
  projects:
    - PROJ
    - BYGR
```

---

## Style Conventions

- Keep UI concerns in `ui/`, data/persistence in `store/`, tmux ops in `tmux/`
- New views follow the Bubble Tea pattern: implement `View() string`, handle `tea.Msg` in `model.go`'s `Update()`
- Lipgloss styles defined in `ui/styles.go` — don't inline styles in view files
- No comments unless the WHY is non-obvious
