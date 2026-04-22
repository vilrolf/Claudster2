# Conductr — Todo Feature Roadmap

## V1 — MVP

### Core todo view
- [ ] New `todos.go` view in `ui/`
- [ ] Toggle with `T` keybind
- [ ] Todos grouped by source (Jira project name or "Manual")
- [ ] Show todo title, source, and status

### Manual todos
- [ ] `a` keybind to add a manual todo (title + optional description)
- [ ] `D` to delete a todo
- [ ] Persist to `~/.claudster-todos.yaml`

### Jira sync
- [ ] Fetch issues assigned to current user on startup
- [ ] Configurable Jira project keys in `~/.claudster.yaml` under `jira.projects`
- [ ] Store fetched todos in `~/.claudster-todos.yaml`
- [ ] Group todos by Jira project in the view

### Run agent on todo
- [ ] `enter` on a todo opens the new-session modal
- [ ] Modal pre-fills the prompt with todo title + description
- [ ] User picks repo(s) to work in (same flow as existing new session)
- [ ] Session spawns with todo context

---

## V2 — Nice to have

- [ ] Manual refresh keybind for Jira sync (not just on startup)
- [ ] Link Jira project to a default Claudster project/repo (skip repo picker)
- [ ] Auto-transition Jira ticket status when session completes (e.g. → In Review)
- [ ] Post a Jira comment when an agent session starts ("Claude working on this")
- [ ] Filter/sort todos (by project, priority, status)
- [ ] Cross-computer sync via configurable todos file path (Dropbox/OneDrive)
- [ ] Due dates visible on todo cards
- [ ] Create a todo from a finished session (retroactive logging)
