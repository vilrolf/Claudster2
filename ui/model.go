package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"claudster/jira"
	"claudster/metrics"
	"claudster/skills"
	"claudster/store"
	"claudster/tmux"
)

var spinner = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

var workingPalette = []lipgloss.Color{
	"#BB9AF7", // violet
	"#9D7CD8", // purple
	"#7AA2F7", // blue
	"#41A6B5", // teal-blue
	"#06B6D4", // cyan
	"#41A6B5", // teal-blue
	"#7AA2F7", // blue
	"#9D7CD8", // purple
}

// ── row types ─────────────────────────────────────────────────────────────────

type rowType int

const (
	rowTypeOverview rowType = iota
	rowTypeGroup
	rowTypeProject
	rowTypeSession
	rowTypeSkillsHeader // non-selectable section divider
	rowTypeSkillScope   // "Global" or a project scope
	rowTypeSkill        // individual skill
)

type sidebarRow struct {
	typ        rowType
	label      string
	groupIdx   int
	projectIdx int
	sessionIdx int
	skillPath  string // absolute path to SKILL.md
	skillScope string // absolute path to the parent skills/ directory
	yPos       int    // terminal Y for mouse hit-testing
}

// ── sidebar mode ──────────────────────────────────────────────────────────────

type sidebarModeType int

const (
	sidebarModeSessions sidebarModeType = iota
	sidebarModeTodos
)

// ── modal ─────────────────────────────────────────────────────────────────────

type modalMode int

const (
	modalNone             modalMode = iota
	modalNewProject                 // N
	modalNewSession                 // n
	modalResumeSession              // r
	modalNewEditorSession           // V / G / T
	modalConfirmDelete              // d on a session
	modalHelp                       // ?
	modalScratchAppend              // S — quick add to scratch
	modalNewSkill                   // a on a skill scope/skill row
	modalConfirmSkillDelete         // d on a skill row
	modalAddTodo                    // a (in todos view)
	modalRunTodoAgent               // enter (in todos view) — multi-step: project → name → dangerous
	modalSetStatus                  // s (in todos view) — pick status with up/down
)

// modalNewProject only needs one step (group name); the rest is done in $EDITOR.
// modalNewSession only needs one step (session name).

type modalState struct {
	mode          modalMode
	targetGroup   string
	targetProject string
	targetKind    string // for modalNewEditorSession: "editor" or "lazygit"
	// skill fields
	targetSkillScope string // for modalNewSkill: absolute path to skills/ directory
	targetSkillName  string // for modalConfirmSkillDelete: skill name to show
	targetSkillDir   string // for modalConfirmSkillDelete: absolute skill directory to remove
	input            textinput.Model
	completions      []string // tab-cycle candidates (group names or project names)
	compIdx          int
	step             int        // 0 = name/project input, 1 = dangerous confirm (or session name for todo)
	pendingName      string     // session name held between steps
	pendingTodo      *store.Todo // todo being run as an agent
	quickSpawn    bool       // skip session name + dangerous confirm, spawn immediately
	statusCursor  int        // for modalSetStatus: 0=unstarted 1=in_progress 2=done
}

func newModalState() modalState {
	ti := textinput.New()
	ti.CharLimit = 200
	return modalState{input: ti}
}

// ── model ─────────────────────────────────────────────────────────────────────

type Model struct {
	config    store.Config
	configErr string
	monitor   *tmux.Monitor

	rows           []sidebarRow
	cursor         int
	expanded       map[string]bool
	groupCollapsed map[int]bool
	yToRow         map[int]int

	modal modalState

	spinFrame     int
	spinTick      int
	claudeStats   metrics.Stats
	dangerousMode bool

	skillsGlobal  []skills.Skill
	skillsProject map[string][]skills.Skill

	sidebarW int
	dashW    int
	dashH    int
	width    int
	height   int

	status    string
	statusExp time.Time

	toasts         []toast
	tmuxBoundCount int

	searchMode bool
	searchStr  string

	// todos
	sidebarMode  sidebarModeType
	todos        store.TodoList
	todoCursor   int
	jiraFetching bool
	jiraTotal    int
	jiraErr      string
}

type tickMsg time.Time
type pollTickMsg struct{}
type pollDoneMsg struct{ doneTransitions []string }
type attachDoneMsg struct{ err error }
type editorDoneMsg struct{}
type metricsMsg metrics.Stats
type popupErrMsg string
type jiraFetchDoneMsg struct {
	todos  []store.Todo
	total  int
	errStr string
}

func New() Model {
	cfg, cfgErr := store.Load()
	todos, _ := store.LoadTodos()
	m := Model{
		config:         cfg,
		monitor:        tmux.NewMonitor(),
		expanded:       make(map[string]bool),
		groupCollapsed: make(map[int]bool),
		yToRow:         make(map[int]int),
		modal:          newModalState(),
		skillsProject:  make(map[string][]skills.Skill),
		todos:          todos,
		jiraFetching:   cfg.Jira.URL != "",
	}
	if cfgErr != nil {
		m.configErr = cfgErr.Error()
	}
	for gi, g := range cfg.Groups {
		for pi := range g.Projects {
			m.expanded[expandKey(gi, pi)] = true
		}
	}
	skills.Bootstrap()
	m.scanSkills()
	m.rebuildRows()
	if os.Getenv("TMUX") != "" {
		exec.Command("tmux", "set-option", "-g", "mouse", "on").Run()
		exec.Command("tmux", "bind-key", "-n", "C-q", "switch-client", "-l").Run()
	}
	return m
}

func (m *Model) scanSkills() {
	m.skillsGlobal = skills.ScanGlobal()
	seen := make(map[string]bool)
	for _, g := range m.config.Groups {
		for _, p := range g.Projects {
			repo := skills.ExpandPath(p.PrimaryRepo())
			if seen[repo] {
				continue
			}
			seen[repo] = true
			m.skillsProject[repo] = skills.ScanProject(repo, p.Name)
		}
	}
}

func fetchJiraCmd(cfg store.JiraConfig) tea.Cmd {
	return func() tea.Msg {
		todos, total, err := jira.FetchAssigned(cfg)
		errStr := ""
		if err != nil {
			errStr = err.Error()
		}
		return jiraFetchDoneMsg{todos: todos, total: total, errStr: errStr}
	}
}

func (m *Model) allSessionNames() map[string]bool {
	names := make(map[string]bool)
	for _, g := range m.config.Groups {
		for _, p := range g.Projects {
			for _, s := range p.Sessions {
				names[s.Name] = true
			}
		}
	}
	return names
}

func expandKey(gi, pi int) string {
	return fmt.Sprintf("%d:%d", gi, pi)
}

func (m *Model) rebuildRows() {
	var rows []sidebarRow
	yPos := 2 // Y=0 top border, Y=1 title, content from Y=2

	rows = append(rows, sidebarRow{
		typ: rowTypeOverview, label: "overview",
		groupIdx: -1, projectIdx: -1, sessionIdx: -1, yPos: yPos,
	})
	yPos += 2 // overview row + blank spacer

	for gi, g := range m.config.Groups {
		if gi > 0 {
			yPos++ // blank line between groups
		}
		rows = append(rows, sidebarRow{
			typ: rowTypeGroup, label: g.Name,
			groupIdx: gi, projectIdx: -1, sessionIdx: -1, yPos: yPos,
		})
		yPos++

		if m.groupCollapsed[gi] {
			continue
		}

		for pi, p := range g.Projects {
			rows = append(rows, sidebarRow{
				typ: rowTypeProject, label: p.Name,
				groupIdx: gi, projectIdx: pi, sessionIdx: -1, yPos: yPos,
			})
			yPos++

			if m.expanded[expandKey(gi, pi)] {
				for si, s := range p.Sessions {
					rows = append(rows, sidebarRow{
						typ: rowTypeSession, label: s.Name,
						groupIdx: gi, projectIdx: pi, sessionIdx: si, yPos: yPos,
					})
					yPos++
				}
			}
		}
	}

	// Skills section
	yPos++
	rows = append(rows, sidebarRow{
		typ: rowTypeSkillsHeader, label: "skills",
		groupIdx: -1, projectIdx: -1, sessionIdx: -1, yPos: yPos,
	})
	yPos++

	globalDir := skills.GlobalDir()
	rows = append(rows, sidebarRow{
		typ: rowTypeSkillScope, label: "Global",
		skillScope: globalDir,
		groupIdx: -1, projectIdx: -1, sessionIdx: -1, yPos: yPos,
	})
	yPos++
	for _, sk := range m.skillsGlobal {
		rows = append(rows, sidebarRow{
			typ: rowTypeSkill, label: sk.Name,
			skillPath: sk.FilePath, skillScope: sk.ScopeDir,
			groupIdx: -1, projectIdx: -1, sessionIdx: -1, yPos: yPos,
		})
		yPos++
	}

	type scopeEntry struct {
		key    string
		label  string
		skills []skills.Skill
	}
	var projectScopes []scopeEntry
	seenScope := make(map[string]bool)
	for _, g := range m.config.Groups {
		for _, p := range g.Projects {
			repo := skills.ExpandPath(p.PrimaryRepo())
			if seenScope[repo] {
				continue
			}
			seenScope[repo] = true
			projSkills := m.skillsProject[repo]
			if len(projSkills) == 0 {
				continue
			}
			projectScopes = append(projectScopes, scopeEntry{
				key:    repo,
				label:  p.Name,
				skills: projSkills,
			})
		}
	}
	for _, sg := range projectScopes {
		rows = append(rows, sidebarRow{
			typ: rowTypeSkillScope, label: sg.label,
			skillScope: skills.ProjectDir(sg.key),
			groupIdx: -1, projectIdx: -1, sessionIdx: -1, yPos: yPos,
		})
		yPos++
		for _, sk := range sg.skills {
			rows = append(rows, sidebarRow{
				typ: rowTypeSkill, label: sk.Name,
				skillPath: sk.FilePath, skillScope: sk.ScopeDir,
				groupIdx: -1, projectIdx: -1, sessionIdx: -1, yPos: yPos,
			})
			yPos++
		}
	}

	m.rows = rows
	m.yToRow = make(map[int]int, len(rows))
	for i, r := range rows {
		m.yToRow[r.yPos] = i
	}
	m.clampCursor()
}

func nonSelectable(t rowType) bool {
	return t == rowTypeSkillsHeader
}

func (m *Model) clampCursor() {
	if len(m.rows) == 0 {
		return
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	for m.cursor < len(m.rows) && nonSelectable(m.rows[m.cursor].typ) {
		m.cursor++
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
		for m.cursor > 0 && nonSelectable(m.rows[m.cursor].typ) {
			m.cursor--
		}
	}
	// If still negative (only group rows exist), leave at 0 — View guards against this
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m Model) pollCmd() tea.Cmd {
	var allNames []string
	var claudeNames []string
	for _, g := range m.config.Groups {
		for _, p := range g.Projects {
			for _, s := range p.Sessions {
				allNames = append(allNames, s.Name)
				if !s.IsToolSession() {
					claudeNames = append(claudeNames, s.Name)
				}
			}
		}
	}
	prev := make(map[string]tmux.Status, len(claudeNames))
	for _, n := range claudeNames {
		prev[n] = m.monitor.Get(n).Status
	}
	monitor := m.monitor
	return func() tea.Msg {
		monitor.Poll(allNames)
		var done []string
		for _, n := range claudeNames {
			if prev[n] == tmux.StatusWorking && monitor.Get(n).Status == tmux.StatusDone {
				done = append(done, n)
			}
		}
		return pollDoneMsg{doneTransitions: done}
	}
}

const defaultSidebarW = 32
const minSidebarW = 16
const maxSidebarW = 60

func (m *Model) recalcLayout() {
	if m.width == 0 {
		return
	}
	m.sidebarW = m.config.UI.SidebarWidth
	if m.sidebarW < minSidebarW {
		m.sidebarW = defaultSidebarW
	}
	if m.sidebarW > maxSidebarW {
		m.sidebarW = maxSidebarW
	}
	m.dashW = m.width - m.sidebarW - 6
	if m.dashW < 10 {
		m.dashW = 10
	}
	m.dashH = m.height - 3
	if m.dashH < 1 {
		m.dashH = 1
	}
}

func (m *Model) setStatus(msg string) {
	m.status = msg
	m.statusExp = time.Now().Add(3 * time.Second)
}

func tick() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tick(), m.pollCmd()}
	if m.config.Jira.URL != "" {
		cmds = append(cmds, fetchJiraCmd(m.config.Jira))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcLayout()

	case tickMsg:
		m.spinFrame = (m.spinFrame + 1) % len(spinner)
		m.spinTick++
		m.tickToasts()
		cmds := []tea.Cmd{tick()}
		if m.spinTick%67 == 0 { // every ~10 s
			cmds = append(cmds, func() tea.Msg {
				return metricsMsg(metrics.Collect())
			})
		}
		return m, tea.Batch(cmds...)

	case pollTickMsg:
		return m, m.pollCmd()

	case pollDoneMsg:
		for _, name := range msg.doneTransitions {
			m.addToast(name)
		}
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return pollTickMsg{}
		})

	case metricsMsg:
		m.claudeStats = metrics.Stats(msg)

	case jiraFetchDoneMsg:
		m.jiraFetching = false
		m.jiraErr = msg.errStr
		m.jiraTotal = msg.total
		if len(msg.todos) > 0 {
			m.todos.MergeJiraTodos(msg.todos)
			store.SaveTodos(m.todos)
		}

	case attachDoneMsg:
		return m, tea.EnableMouseAllMotion

	case popupErrMsg:
		m.setStatus("popup error: " + string(msg))
		return m, nil

	case editorDoneMsg:
		cfg, err := store.Load()
		if err != nil {
			m.configErr = err.Error()
		} else {
			m.configErr = ""
			m.config = cfg
			for gi, g := range m.config.Groups {
				for pi := range g.Projects {
					k := expandKey(gi, pi)
					if _, ok := m.expanded[k]; !ok {
						m.expanded[k] = true
					}
				}
			}
		}
		m.scanSkills()
		m.rebuildRows()
		m.recalcLayout()
		return m, nil

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionPress {
				return m.handleMouseClick(msg.X, msg.Y)
			}
		case tea.MouseButtonWheelUp:
			if m.sidebarMode == sidebarModeTodos {
				m.todoMoveUp()
			} else {
				m.moveUp()
			}
			return m, nil
		case tea.MouseButtonWheelDown:
			if m.sidebarMode == sidebarModeTodos {
				m.todoMoveDown()
			} else {
				m.moveDown()
			}
			return m, nil
		}

	case tea.KeyMsg:
		if m.modal.mode != modalNone {
			return m.handleModalKey(msg)
		}
		if m.searchMode {
			return m.handleSearchKey(msg)
		}
		return m.handleKey(msg)
	}

	return m, nil
}

// ── keyboard ──────────────────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c", "ctrl+q":
		return m, tea.Quit

	case "/":
		if m.sidebarMode == sidebarModeSessions {
			m.searchMode = true
			m.searchStr = ""
			return m, nil
		}

	case "ctrl+d":
		if m.sidebarMode == sidebarModeTodos {
			for i := 0; i < 5; i++ {
				m.todoMoveDown()
			}
		} else {
			for i := 0; i < 5; i++ {
				m.moveDown()
			}
		}

	case "ctrl+u":
		if m.sidebarMode == sidebarModeTodos {
			for i := 0; i < 5; i++ {
				m.todoMoveUp()
			}
		} else {
			for i := 0; i < 5; i++ {
				m.moveUp()
			}
		}

	case "tab":
		if m.sidebarMode == sidebarModeSessions {
			m.sidebarMode = sidebarModeTodos
			m.todoCursor = -1 // start on overview
		} else {
			m.sidebarMode = sidebarModeSessions
		}
		return m, nil
	}

	if m.sidebarMode == sidebarModeTodos {
		return m.handleTodosKey(msg)
	}

	switch msg.String() {
	case "j", "down":
		m.moveDown()

	case "k", "up":
		m.moveUp()

	case "space":
		m.toggleExpand()

	case "enter":
		return m.handleEnter()

	case "n":
		return m.startNewSession(), nil

	case "r":
		return m.startResumeSession(), nil

	case "N":
		return m.startNewProject(), nil

	case "a":
		return m.startNewSkill(), nil

	case "d":
		return m.startConfirmDelete(), nil

	case "p":
		m.dangerousMode = !m.dangerousMode

	case "P":
		return m.restartCurrentSession()

	case "v":
		if m.cursor < len(m.rows) && m.rows[m.cursor].typ == rowTypeSkill {
			return m.openSkillInEditor(m.rows[m.cursor])
		}
		return m.openInEditor()

	case "V":
		return m.startNewToolSession("editor"), nil

	case "s":
		return m.openScratchAppend()

	case "S":
		return m.openScratch()

	case "c":
		return m.enterCopyMode()

	case "G":
		return m.openInGitClient()

	case "t":
		return m.openInTerminal()

	case "T":
		return m.startNewToolSession("terminal"), nil

	case "?":
		m.modal.mode = modalHelp
		return m, nil

	case "e":
		editor := resolveEditor(m.config.UI.Editor)
		var cmd *exec.Cmd
		if isVSCode(editor) {
			cmd = exec.Command(editor, "--wait", wslPath(store.ConfigPath()))
		} else {
			cmd = exec.Command(editor, store.ConfigPath())
		}
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return editorDoneMsg{}
		})

	case "]":
		w := m.sidebarW + 2
		if w > maxSidebarW {
			w = maxSidebarW
		}
		m.config.UI.SidebarWidth = w
		m.recalcLayout()
		store.Save(m.config)

	case "[":
		w := m.sidebarW - 2
		if w < minSidebarW {
			w = minSidebarW
		}
		m.config.UI.SidebarWidth = w
		m.recalcLayout()
		store.Save(m.config)

	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(msg.String()[0] - '1')
		if idx < len(m.toasts) {
			name := m.jumpToToast(idx)
			if name != "" {
				return m, switchOrAttach(name)
			}
		}
	}

	return m, nil
}

func (m *Model) jumpToTop() {
	for i, r := range m.rows {
		if !nonSelectable(r.typ) {
			m.cursor = i
			return
		}
	}
}

func (m *Model) jumpToBottom() {
	for i := len(m.rows) - 1; i >= 0; i-- {
		if !nonSelectable(m.rows[i].typ) {
			m.cursor = i
			return
		}
	}
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.searchMode = false
		m.searchStr = ""
	case "enter":
		m.searchMode = false
	case "backspace", "ctrl+h":
		if len(m.searchStr) > 0 {
			m.searchStr = m.searchStr[:len([]rune(m.searchStr))-1]
			m.jumpToSearch()
		}
	default:
		if len(msg.Runes) > 0 {
			m.searchStr += string(msg.Runes)
			m.jumpToSearch()
		}
	}
	return m, nil
}

func (m *Model) jumpToSearch() {
	if m.searchStr == "" {
		return
	}
	q := strings.ToLower(m.searchStr)
	for i, r := range m.rows {
		if nonSelectable(r.typ) {
			continue
		}
		if strings.Contains(strings.ToLower(r.label), q) {
			m.cursor = i
			return
		}
	}
}

func (m Model) handleTodosKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visibles := visibleTodos(m.todos.Todos, true)
	switch msg.String() {
	case "j", "down":
		if m.todoCursor == -1 && len(visibles) > 0 {
			m.todoCursor = 0
		} else if m.todoCursor >= 0 && m.todoCursor < len(visibles)-1 {
			m.todoCursor++
		}
	case "k", "up":
		if m.todoCursor > 0 {
			m.todoCursor--
		} else if m.todoCursor == 0 {
			m.todoCursor = -1 // back to overview
		}
	case "s":
		if m.todoCursor >= 0 && m.todoCursor < len(visibles) {
			todo := visibles[m.todoCursor]
			statusIdx := map[string]int{
				store.StatusUnstarted:  0,
				store.StatusInProgress: 1,
				store.StatusDone:       2,
				"":                     0,
			}
			m.modal.mode = modalSetStatus
			m.modal.pendingTodo = &todo
			m.modal.statusCursor = statusIdx[todo.Status]
		}
	case "enter":
		if m.todoCursor >= 0 && m.todoCursor < len(visibles) {
			todo := visibles[m.todoCursor]
			if todo.SessionName != "" {
				return m, switchOrAttach(todo.SessionName)
			}
			if todo.GroupName != "" && todo.ProjectName != "" {
				return m.quickSpawnTodo(todo)
			}
			// No project linked yet — ask for it, then auto-spawn
			m.modal.pendingTodo = &todo
			m.modal.mode = modalRunTodoAgent
			m.modal.quickSpawn = true
			m.modal.step = 0
			m.modal.completions = nil
			m.modal.input.Placeholder = existingProjectHint(m.config)
			m.modal.input.SetValue("")
			m.modal.input.Focus()
		}
	case "n":
		if m.todoCursor >= 0 && m.todoCursor < len(visibles) {
			todo := visibles[m.todoCursor]
			m.modal.pendingTodo = &todo
			m.modal.mode = modalRunTodoAgent
			m.modal.quickSpawn = false
			m.modal.completions = nil
			if todo.GroupName != "" && todo.ProjectName != "" {
				m.modal.targetGroup = todo.GroupName
				m.modal.targetProject = todo.ProjectName
				m.modal.step = 1
				m.modal.input.Placeholder = "e.g. " + todoSessionName(&todo)
				m.modal.input.SetValue(todoSessionName(&todo))
			} else {
				m.modal.step = 0
				m.modal.input.Placeholder = existingProjectHint(m.config)
				m.modal.input.SetValue("")
			}
			m.modal.input.Focus()
		}
	case "v":
		if m.todoCursor >= 0 && m.todoCursor < len(visibles) {
			return m.openTodoInEditor(visibles[m.todoCursor])
		}
	case "G":
		if m.todoCursor >= 0 && m.todoCursor < len(visibles) {
			return m.openTodoInGitClient(visibles[m.todoCursor])
		}
	case "t":
		if m.todoCursor >= 0 && m.todoCursor < len(visibles) {
			return m.openTodoInTerminal(visibles[m.todoCursor])
		}
	case "o":
		if m.todoCursor >= 0 && m.todoCursor < len(visibles) {
			todo := visibles[m.todoCursor]
			if todo.JiraKey != "" {
				issueURL := strings.TrimRight(m.config.Jira.URL, "/") + "/browse/" + todo.JiraKey
				openURL(issueURL)
			}
		}
	case "a":
		m.modal.mode = modalAddTodo
		m.modal.step = 0
		m.modal.input.Placeholder = "e.g. Review PR from Tom"
		m.modal.input.SetValue("")
		m.modal.input.Focus()
	case "d", "D":
		if m.todoCursor >= 0 && m.todoCursor < len(visibles) {
			m.modal.mode = modalConfirmDelete
		}
	case "?":
		m.modal.mode = modalHelp
	}
	return m, nil
}

func removeTodoByID(todos []store.Todo, id string) []store.Todo {
	out := make([]store.Todo, 0, len(todos))
	for _, t := range todos {
		if t.ID != id {
			out = append(out, t)
		}
	}
	return out
}

func existingProjectHint(cfg store.Config) string {
	var names []string
	for _, g := range cfg.Groups {
		for _, p := range g.Projects {
			names = append(names, g.Name+"/"+p.Name)
		}
	}
	if len(names) == 0 {
		return "e.g. work/myapp"
	}
	if len(names) > 3 {
		names = names[:3]
	}
	return strings.Join(names, ", ")
}

func (m *Model) moveDown() {
	next := m.cursor + 1
	for next < len(m.rows) && nonSelectable(m.rows[next].typ) {
		next++
	}
	if next < len(m.rows) {
		m.cursor = next
	}
}

func (m *Model) moveUp() {
	prev := m.cursor - 1
	for prev >= 0 && nonSelectable(m.rows[prev].typ) {
		prev--
	}
	if prev >= 0 {
		m.cursor = prev
	}
}

func (m *Model) todoMoveDown() {
	visibles := visibleTodos(m.todos.Todos, true)
	if m.todoCursor == -1 && len(visibles) > 0 {
		m.todoCursor = 0
	} else if m.todoCursor >= 0 && m.todoCursor < len(visibles)-1 {
		m.todoCursor++
	}
}

func (m *Model) todoMoveUp() {
	if m.todoCursor > 0 {
		m.todoCursor--
	} else if m.todoCursor == 0 {
		m.todoCursor = -1
	}
}

func (m *Model) toggleExpand() {
	if m.cursor >= len(m.rows) {
		return
	}
	row := m.rows[m.cursor]
	switch row.typ {
	case rowTypeGroup:
		m.groupCollapsed[row.groupIdx] = !m.groupCollapsed[row.groupIdx]
		m.rebuildRows()
	case rowTypeProject, rowTypeSession:
		key := expandKey(row.groupIdx, row.projectIdx)
		m.expanded[key] = !m.expanded[key]
		m.rebuildRows()
	}
}

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.rows) {
		return m, nil
	}
	row := m.rows[m.cursor]

	switch row.typ {
	case rowTypeGroup:
		m.toggleExpand()
		return m, nil

	case rowTypeProject:
		m.toggleExpand()
		return m, nil

	case rowTypeSession:
		if !m.monitor.Exists(row.label) {
			// Start then attach
			proj := &m.config.Groups[row.groupIdx].Projects[row.projectIdx]
			if err := tmux.NewSession(row.label, proj.PrimaryRepo(), proj.AdditionalRepos(), m.dangerousMode); err != nil {
				m.setStatus(fmt.Sprintf("error starting session: %v", err))
				return m, nil
			}
		}
		return m, switchOrAttach(row.label)

	case rowTypeSkill:
		return m.openSkillInEditor(row)
	}

	return m, nil
}

// switchOrAttach switches to the named session. When running inside tmux it
// uses switch-client (claudster stays alive in its own window). Outside tmux
// it falls back to attach-session via ExecProcess.
func switchOrAttach(name string) tea.Cmd {
	if os.Getenv("TMUX") != "" {
		return func() tea.Msg {
			tmux.SwitchTo(name)
			return nil
		}
	}
	script := fmt.Sprintf(
		"tmux bind-key -n C-q detach-client; tmux attach-session -t '%s'; tmux unbind-key -n C-q 2>/dev/null",
		name,
	)
	cmd := exec.Command("sh", "-c", script)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return attachDoneMsg{err: err}
	})
}

func (m Model) startNewSession() Model {
	if m.cursor >= len(m.rows) {
		return m
	}
	row := m.rows[m.cursor]
	if row.typ != rowTypeProject && row.typ != rowTypeSession {
		return m
	}
	gi, pi := row.groupIdx, row.projectIdx
	m.modal.mode = modalNewSession
	m.modal.step = 0
	m.modal.pendingName = ""
	m.modal.targetGroup = m.config.Groups[gi].Name
	m.modal.targetProject = m.config.Groups[gi].Projects[pi].Name
	m.modal.input.Placeholder = "e.g. auth-refactor"
	m.modal.input.SetValue("")
	m.modal.input.Focus()
	return m
}

func (m Model) startResumeSession() Model {
	if m.cursor >= len(m.rows) {
		return m
	}
	row := m.rows[m.cursor]
	if row.typ != rowTypeProject && row.typ != rowTypeSession {
		return m
	}
	gi, pi := row.groupIdx, row.projectIdx
	m.modal.mode = modalResumeSession
	m.modal.step = 0
	m.modal.pendingName = ""
	m.modal.targetGroup = m.config.Groups[gi].Name
	m.modal.targetProject = m.config.Groups[gi].Projects[pi].Name
	m.modal.input.Placeholder = "e.g. auth-refactor"
	m.modal.input.SetValue("")
	m.modal.input.Focus()
	return m
}

func (m Model) startConfirmDelete() Model {
	if m.cursor >= len(m.rows) {
		return m
	}
	row := m.rows[m.cursor]
	switch row.typ {
	case rowTypeSession:
		m.modal.mode = modalConfirmDelete
	case rowTypeSkill:
		m.modal.mode = modalConfirmSkillDelete
		m.modal.targetSkillName = row.label
		m.modal.targetSkillDir = filepath.Dir(row.skillPath)
	}
	return m
}

func (m Model) startNewSkill() Model {
	if m.cursor >= len(m.rows) {
		return m
	}
	row := m.rows[m.cursor]
	var scopeDir string
	switch row.typ {
	case rowTypeSkillScope:
		scopeDir = row.skillScope
	case rowTypeSkill:
		scopeDir = row.skillScope
	default:
		scopeDir = skills.GlobalDir()
	}
	m.modal.mode = modalNewSkill
	m.modal.targetSkillScope = scopeDir
	m.modal.input.Placeholder = "e.g. typescript-style"
	m.modal.input.SetValue("")
	m.modal.input.Focus()
	return m
}

func (m Model) openSkillInEditor(row sidebarRow) (tea.Model, tea.Cmd) {
	editor := resolveEditor(m.config.UI.Editor)
	var cmd *exec.Cmd
	if isVSCode(editor) {
		cmd = exec.Command(editor, "--wait", wslPath(row.skillPath))
	} else {
		cmd = exec.Command(editor, row.skillPath)
	}
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{}
	})
}

func (m *Model) deleteSelectedSkill() {
	if m.cursor >= len(m.rows) {
		return
	}
	row := m.rows[m.cursor]
	if row.typ != rowTypeSkill {
		return
	}
	if err := skills.Delete(filepath.Dir(row.skillPath)); err != nil {
		m.setStatus(fmt.Sprintf("error deleting skill: %v", err))
	}
	m.scanSkills()
	if m.cursor > 0 {
		m.cursor--
	}
	m.rebuildRows()
}

func (m Model) enterCopyMode() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.rows) {
		return m, nil
	}
	row := m.rows[m.cursor]
	if row.typ != rowTypeSession {
		return m, nil
	}
	name := row.label
	if os.Getenv("TMUX") == "" || !m.monitor.Exists(name) {
		return m, nil
	}
	return m, func() tea.Msg {
		exec.Command("tmux", "copy-mode", "-t", name).Run()
		exec.Command("tmux", "switch-client", "-t", name).Run()
		return nil
	}
}

func (m Model) scratchGroupProject() (string, string, bool) {
	if m.cursor >= len(m.rows) {
		return "", "", false
	}
	row := m.rows[m.cursor]
	if row.typ != rowTypeProject && row.typ != rowTypeSession {
		return "", "", false
	}
	g := m.config.Groups[row.groupIdx].Name
	p := m.config.Groups[row.groupIdx].Projects[row.projectIdx].Name
	return g, p, true
}

func (m Model) openScratch() (tea.Model, tea.Cmd) {
	group, project, ok := m.scratchGroupProject()
	if !ok {
		return m, nil
	}
	path := store.ScratchPath(group, project)
	editor := resolveEditor(m.config.UI.Editor)
	if os.Getenv("TMUX") == "" {
		var cmd *exec.Cmd
		if isVSCode(editor) {
			cmd = exec.Command(editor, "--wait", wslPath(path))
		} else {
			cmd = exec.Command(editor, path)
		}
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{} })
	}
	title := " ✎ " + project + " scratch "
	return m, func() tea.Msg {
		exec.Command("tmux", "display-popup",
			"-E", "-T", title, "-w", "80%", "-h", "80%",
			editor, path,
		).Run()
		return nil
	}
}

func (m Model) openScratchAppend() (tea.Model, tea.Cmd) {
	group, project, ok := m.scratchGroupProject()
	if !ok {
		return m, nil
	}
	path := store.ScratchPath(group, project)
	title := " ✎ " + project + " scratch "
	if os.Getenv("TMUX") == "" {
		m.setStatus("scratch preview requires tmux")
		return m, nil
	}
	editor := resolveEditor(m.config.UI.Editor)
	var popupArgs []string
	if isVSCode(editor) {
		popupArgs = []string{"display-popup", "-E", "-T", title, "-w", "70%", "-h", "60%", editor, "--wait", wslPath(path)}
	} else {
		popupArgs = []string{"display-popup", "-E", "-T", title, "-w", "70%", "-h", "60%", editor, "+999999", path}
	}
	return m, func() tea.Msg {
		exec.Command("tmux", popupArgs...).Run()
		return nil
	}
}

func (m Model) startNewToolSession(kind string) Model {
	if m.cursor >= len(m.rows) {
		return m
	}
	row := m.rows[m.cursor]
	if row.typ == rowTypeGroup {
		return m
	}
	gi, pi := row.groupIdx, row.projectIdx
	m.modal.mode = modalNewEditorSession
	m.modal.targetGroup = m.config.Groups[gi].Name
	m.modal.targetProject = m.config.Groups[gi].Projects[pi].Name
	m.modal.targetKind = kind
	placeholder := "e.g. edit"
	switch kind {
	case "lazygit":
		placeholder = "e.g. git"
	case "terminal":
		placeholder = "e.g. shell"
	}
	m.modal.input.Placeholder = placeholder
	m.modal.input.SetValue("")
	m.modal.input.Focus()
	return m
}

func (m Model) startNewProject() Model {
	m.modal.mode = modalNewProject
	m.modal.input.Placeholder = existingGroupHint(m.config)
	m.modal.input.SetValue("")
	m.modal.input.Focus()
	return m
}

func (m Model) openInTerminal() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.rows) {
		return m, nil
	}
	row := m.rows[m.cursor]
	var primaryRepo string
	switch row.typ {
	case rowTypeProject:
		primaryRepo = m.config.Groups[row.groupIdx].Projects[row.projectIdx].PrimaryRepo()
	case rowTypeSession:
		primaryRepo = m.config.Groups[row.groupIdx].Projects[row.projectIdx].PrimaryRepo()
	default:
		m.setStatus("select a project or session first")
		return m, nil
	}
	if os.Getenv("TMUX") == "" {
		m.setStatus("t requires claudster to be running inside tmux")
		return m, nil
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}
	expanded := tmux.ExpandPath(primaryRepo)
	return m, func() tea.Msg {
		out, err := exec.Command("tmux", "display-popup",
			"-E", "-d", expanded,
			"-T", " ctrl-d to close ",
			"-w", "80%", "-h", "80%",
			shell,
		).CombinedOutput()
		if err != nil {
			return popupErrMsg(string(out) + ": " + err.Error())
		}
		return nil
	}
}

func (m Model) openInGitClient() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.rows) {
		return m, nil
	}
	row := m.rows[m.cursor]
	var primaryRepo string
	switch row.typ {
	case rowTypeProject:
		primaryRepo = m.config.Groups[row.groupIdx].Projects[row.projectIdx].PrimaryRepo()
	case rowTypeSession:
		primaryRepo = m.config.Groups[row.groupIdx].Projects[row.projectIdx].PrimaryRepo()
	default:
		m.setStatus("select a project or session first")
		return m, nil
	}
	path := tmux.ExpandPath(primaryRepo)
	client := m.config.UI.GitClient
	if client == "" {
		client = "lazygit"
	}
	if client == "github-desktop" {
		var cmd *exec.Cmd
		if runtime.GOOS == "darwin" {
			cmd = exec.Command("open", "-a", "GitHub Desktop", path)
		} else {
			// Linux / WSL: GitHub Desktop installs a 'github' CLI helper.
			cmd = exec.Command("github", path)
		}
		// GitHub Desktop is a GUI app — fire and forget, don't block the TUI.
		return m, func() tea.Msg {
			if err := cmd.Run(); err != nil {
				return popupErrMsg("could not open GitHub Desktop: " + err.Error())
			}
			return nil
		}
	}
	cmd := exec.Command(client, "-p", path)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{}
	})
}

func (m Model) openInEditor() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.rows) {
		return m, nil
	}
	row := m.rows[m.cursor]
	var primaryRepo string
	switch row.typ {
	case rowTypeProject:
		proj := m.config.Groups[row.groupIdx].Projects[row.projectIdx]
		primaryRepo = proj.PrimaryRepo()
	case rowTypeSession:
		proj := m.config.Groups[row.groupIdx].Projects[row.projectIdx]
		primaryRepo = proj.PrimaryRepo()
	default:
		m.setStatus("select a project or session first")
		return m, nil
	}
	path := tmux.ExpandPath(primaryRepo)
	editor := resolveEditor(m.config.UI.Editor)
	var cmd *exec.Cmd
	if isVSCode(editor) {
		// Open folder in VS Code without --wait so claudster stays running.
		cmd = exec.Command(editor, "--new-window", wslPath(path))
	} else {
		cmd = exec.Command(editor, path)
	}
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{}
	})
}

func (m Model) restartCurrentSession() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.rows) {
		return m, nil
	}
	row := m.rows[m.cursor]
	if row.typ != rowTypeSession || !m.monitor.Exists(row.label) {
		m.setStatus("select a running session first")
		return m, nil
	}
	name := row.label
	dangerous := m.dangerousMode
	proj := m.config.Groups[row.groupIdx].Projects[row.projectIdx]
	primaryRepo := proj.PrimaryRepo()
	mode := "normal"
	if dangerous {
		mode = "bypass"
	}
	m.setStatus(fmt.Sprintf("restarting %s in %s mode…", name, mode))
	return m, func() tea.Msg {
		convoID := metrics.LatestConvoID(primaryRepo)
		tmux.RestartSession(name, convoID, dangerous)
		return nil
	}
}

func (m *Model) deleteSelected() {
	if m.cursor >= len(m.rows) {
		return
	}
	row := m.rows[m.cursor]
	if row.typ != rowTypeSession {
		return
	}
	tmux.KillSession(row.label)
	m.config.RemoveSession(
		m.config.Groups[row.groupIdx].Name,
		m.config.Groups[row.groupIdx].Projects[row.projectIdx].Name,
		row.label,
	)
	store.Save(m.config)
	if m.cursor > 0 {
		m.cursor--
	}
	m.rebuildRows()
}

func (m *Model) deleteSelectedTodo() {
	visibles := visibleTodos(m.todos.Todos, true)
	if m.todoCursor >= len(visibles) {
		return
	}
	id := visibles[m.todoCursor].ID
	m.todos.Todos = removeTodoByID(m.todos.Todos, id)
	store.SaveTodos(m.todos)
	if m.todoCursor >= len(m.todos.Todos) && m.todoCursor > 0 {
		m.todoCursor = len(m.todos.Todos) - 1
	}
}

// ── mouse ─────────────────────────────────────────────────────────────────────

func (m Model) handleMouseClick(x, y int) (tea.Model, tea.Cmd) {
	if x <= m.sidebarW+1 {
		// Sidebar area
		rowIdx, ok := m.yToRow[y]
		if !ok {
			return m, nil
		}
		row := m.rows[rowIdx]
		switch row.typ {
		case rowTypeOverview:
			m.cursor = rowIdx
		case rowTypeSkillsHeader:
			// no-op
		case rowTypeGroup:
			m.cursor = rowIdx
			m.toggleExpand()
		case rowTypeProject:
			if m.cursor == rowIdx {
				m.toggleExpand()
			} else {
				m.cursor = rowIdx
			}
		case rowTypeSession:
			if m.cursor == rowIdx {
				return m.handleEnter()
			}
			m.cursor = rowIdx
		case rowTypeSkillScope:
			m.cursor = rowIdx
		case rowTypeSkill:
			if m.cursor == rowIdx {
				return m.openSkillInEditor(row)
			}
			m.cursor = rowIdx
		}
		return m, nil
	}

	// Dashboard area — hit-test against grouped card layout
	targetName := m.hitTestDashboardCard(x, y)
	if targetName == "" {
		return m, nil
	}
	for i, r := range m.rows {
		if r.typ == rowTypeSession && r.label == targetName {
			if m.cursor == i {
				return m.handleEnter()
			}
			m.cursor = i
			return m, nil
		}
	}
	return m, nil
}

// hitTestDashboardCard returns the session name for a click at (absX, absY),
// mirroring the grouped geometry of renderOverview + renderCardGrid exactly.
func (m Model) hitTestDashboardCard(absX, absY int) string {
	const colW = cardContentW + 4
	const cardH = cardContentH + 2

	// Dashboard panel content starts one column after the left border.
	contentX := m.sidebarW + 3
	relX := absX - contentX
	if relX < 0 || relX >= m.dashW {
		return ""
	}

	cols := m.dashW / colW
	if cols < 1 {
		cols = 1
	}
	col := relX / colW
	if col >= cols {
		return ""
	}

	// Reproduce the line count of renderOverview before the card grid.
	// Panel top border is row 0; overview content begins at row 1.
	logoH := 6
	if m.dashW < 74 {
		logoH = 2
	}
	nWorking := 0
	hasAnySessions := false
	for _, g := range m.config.Groups {
		for _, p := range g.Projects {
			for _, s := range p.Sessions {
				if s.IsToolSession() {
					continue
				}
				hasAnySessions = true
				if m.monitor.Exists(s.Name) {
					if m.monitor.Get(s.Name).Status == tmux.StatusWorking {
						nWorking++
					}
				}
			}
		}
	}
	gridStartY := 1 + logoH + 1 + 1 + 1 // border + logo + blank + metrics + blank
	if nWorking > 0 {
		gridStartY += 2
	}
	if hasAnySessions {
		gridStartY += 2
	}

	relY := absY - gridStartY
	if relY < 0 {
		return ""
	}

	// Walk the grouped card layout (mirrors renderCardGrid).
	curY := 0
	firstGroup := true
	for _, g := range m.config.Groups {
		var names []string
		for _, p := range g.Projects {
			for _, s := range p.Sessions {
				if s.IsToolSession() {
					continue
				}
				names = append(names, s.Name)
			}
		}
		if len(names) == 0 {
			continue
		}
		if !firstGroup {
			curY++ // one blank line between groups ("\n\n" separator)
		}
		firstGroup = false

		curY++ // group header line

		nRows := (len(names) + cols - 1) / cols
		groupEnd := curY + nRows*cardH
		if relY >= curY && relY < groupEnd {
			idx := ((relY-curY)/cardH)*cols + col
			if idx < len(names) {
				return names[idx]
			}
			return ""
		}
		curY = groupEnd
	}
	return ""
}

// ── modal keyboard ────────────────────────────────────────────────────────────

func (m Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modal.mode == modalSetStatus {
		statuses := []string{store.StatusUnstarted, store.StatusInProgress, store.StatusDone}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.modal.mode = modalNone
		case "j", "down":
			if m.modal.statusCursor < 2 {
				m.modal.statusCursor++
			}
		case "k", "up":
			if m.modal.statusCursor > 0 {
				m.modal.statusCursor--
			}
		case "enter":
			if m.modal.pendingTodo != nil {
				id := m.modal.pendingTodo.ID
				newStatus := statuses[m.modal.statusCursor]
				for i, t := range m.todos.Todos {
					if t.ID == id {
						m.todos.Todos[i].Status = newStatus
						break
					}
				}
				store.SaveTodos(m.todos)
			}
			m.modal.mode = modalNone
		}
		return m, nil
	}

	// Step 2: dangerous mode confirmation for todo run-agent.
	if m.modal.mode == modalRunTodoAgent && m.modal.step == 2 {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.modal.mode = modalNone
			m.modal.step = 0
			return m, nil
		case "y":
			return m.commitTodoSession(true)
		case "n", "enter":
			return m.commitTodoSession(false)
		}
		return m, nil
	}

	// Step 1: dangerous mode confirmation for new/resume session only.
	// (modalAddTodo uses steps 0/1/2 for title/description/project — not dangerous confirm)
	if m.modal.step == 1 && m.modal.mode != modalRunTodoAgent && m.modal.mode != modalAddTodo {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.modal.mode = modalNone
			m.modal.step = 0
			return m, nil
		case "y":
			return m.commitSession(true)
		case "n", "enter":
			return m.commitSession(false)
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "?":
		m.modal.mode = modalNone
		m.modal.input.Blur()
		return m, nil
	case "enter":
		return m.handleModalEnter()
	case "y":
		if m.modal.mode == modalConfirmDelete {
			m.modal.mode = modalNone
			if m.sidebarMode == sidebarModeTodos {
				m.deleteSelectedTodo()
			} else {
				m.deleteSelected()
			}
			return m, nil
		}
		if m.modal.mode == modalConfirmSkillDelete {
			m.modal.mode = modalNone
			m.deleteSelectedSkill()
			return m, nil
		}
	case "tab":
		if m.modal.mode == modalNewProject {
			m.cycleGroupCompletion()
			return m, nil
		}
		if (m.modal.mode == modalRunTodoAgent && m.modal.step == 0) ||
			(m.modal.mode == modalAddTodo && m.modal.step == 2) {
			m.cycleProjectCompletion()
			return m, nil
		}
	}
	// Any non-tab key resets completions so the next Tab re-filters.
	m.modal.completions = nil
	var cmd tea.Cmd
	m.modal.input, cmd = m.modal.input.Update(msg)
	return m, cmd
}

func (m *Model) cycleGroupCompletion() {
	// Build candidate list from groups matching the current prefix.
	prefix := strings.ToLower(m.modal.input.Value())
	if len(m.modal.completions) == 0 {
		for _, g := range m.config.Groups {
			if strings.HasPrefix(strings.ToLower(g.Name), prefix) {
				m.modal.completions = append(m.modal.completions, g.Name)
			}
		}
		m.modal.compIdx = 0
	} else {
		m.modal.compIdx = (m.modal.compIdx + 1) % len(m.modal.completions)
	}
	if len(m.modal.completions) > 0 {
		m.modal.input.SetValue(m.modal.completions[m.modal.compIdx])
		m.modal.input.CursorEnd()
	}
}

func (m *Model) cycleProjectCompletion() {
	prefix := strings.ToLower(m.modal.input.Value())
	if len(m.modal.completions) == 0 {
		for _, g := range m.config.Groups {
			for _, p := range g.Projects {
				full := g.Name + "/" + p.Name
				if strings.HasPrefix(strings.ToLower(full), prefix) {
					m.modal.completions = append(m.modal.completions, full)
				}
			}
		}
		m.modal.compIdx = 0
	} else {
		m.modal.compIdx = (m.modal.compIdx + 1) % len(m.modal.completions)
	}
	if len(m.modal.completions) > 0 {
		m.modal.input.SetValue(m.modal.completions[m.modal.compIdx])
		m.modal.input.CursorEnd()
	}
}

func (m Model) handleModalEnter() (tea.Model, tea.Cmd) {
	val := strings.TrimSpace(m.modal.input.Value())
	// Allow empty value for description and project steps of add-todo.
	if val == "" && !(m.modal.mode == modalAddTodo && m.modal.step >= 1) {
		return m, nil
	}

	switch m.modal.mode {
	case modalAddTodo:
		switch m.modal.step {
		case 0: // title
			m.modal.pendingName = val
			m.modal.step = 1
			m.modal.input.Placeholder = "optional — enter to skip"
			m.modal.input.SetValue("")
			m.modal.completions = nil
			m.modal.input.Focus()
			return m, nil
		case 1: // description
			m.modal.targetKind = val // reuse targetKind to stash description
			m.modal.step = 2
			m.modal.input.Placeholder = existingProjectHint(m.config)
			m.modal.input.SetValue("")
			m.modal.completions = nil
			m.modal.input.Focus()
			return m, nil
		case 2: // project
			group, proj := resolveProjectInput(val, m.config)
			if group == "" && val != "" {
				m.setStatus("project not found — use group/project or tab to complete")
				return m, nil
			}
			t := store.Todo{
				ID:          store.NewTodoID(),
				Title:       m.modal.pendingName,
				Description: m.modal.targetKind,
				Source:      "manual",
				GroupName:   group,
				ProjectName: proj,
				CreatedAt:   time.Now(),
			}
			m.todos.Todos = append(m.todos.Todos, t)
			store.SaveTodos(m.todos)
			m.modal.mode = modalNone
			m.modal.step = 0
			m.modal.pendingName = ""
			m.modal.targetKind = ""
			m.modal.input.Blur()
			m.todoCursor = len(m.todos.Todos) - 1
			return m, nil
		}

	case modalRunTodoAgent:
		switch m.modal.step {
		case 0:
			// val = "group/project" — resolve to group+project
			group, proj := resolveProjectInput(val, m.config)
			if group == "" {
				m.setStatus("project not found — use group/project format or tab to complete")
				return m, nil
			}
			m.modal.targetGroup = group
			m.modal.targetProject = proj
			m.modal.completions = nil
			if m.modal.quickSpawn {
				// Project was the only missing piece — spawn immediately
				m.modal.mode = modalNone
				todo := m.modal.pendingTodo
				m.modal.pendingTodo = nil
				if todo != nil {
					todo.GroupName = group
					todo.ProjectName = proj
					return m.quickSpawnTodo(*todo)
				}
				return m, nil
			}
			m.modal.step = 1
			m.modal.input.SetValue(todoSessionName(m.modal.pendingTodo))
			m.modal.input.Focus()
			return m, nil
		case 1:
			// val = session name
			if m.allSessionNames()[val] {
				m.setStatus(fmt.Sprintf("session %q already exists", val))
				return m, nil
			}
			m.modal.pendingName = val
			m.modal.step = 2
			m.modal.input.Blur()
			return m, nil
		}

	case modalNewSkill:
		name := val
		path, err := skills.Create(m.modal.targetSkillScope, name)
		m.modal.mode = modalNone
		m.modal.input.Blur()
		if err != nil {
			m.setStatus(fmt.Sprintf("error creating skill: %v", err))
			return m, nil
		}
		m.scanSkills()
		m.rebuildRows()
		editor := resolveEditor(m.config.UI.Editor)
		var editorCmd *exec.Cmd
		if isVSCode(editor) {
			editorCmd = exec.Command(editor, "--wait", wslPath(path))
		} else {
			editorCmd = exec.Command(editor, path)
		}
		return m, tea.ExecProcess(editorCmd, func(err error) tea.Msg {
			return editorDoneMsg{}
		})

	case modalScratchAppend:
		m.modal.mode = modalNone
		m.modal.input.Blur()
		if err := store.AppendScratch(m.modal.targetGroup, m.modal.targetProject, val); err != nil {
			m.setStatus(fmt.Sprintf("scratch error: %v", err))
		} else {
			m.setStatus("added to scratch")
		}
		return m, nil

	case modalNewProject:
		// val = group name. Insert template + open editor.
		group := val
		line, err := store.InsertProjectTemplate(group)
		m.modal.mode = modalNone
		m.modal.input.Blur()
		if err != nil {
			m.setStatus(fmt.Sprintf("error: %v", err))
			return m, nil
		}
		editor := resolveEditor(m.config.UI.Editor)
		var editorCmd *exec.Cmd
		if isVSCode(editor) {
			// VS Code uses --goto file:line syntax; --wait so we reload after edits.
			editorCmd = exec.Command(editor, "--wait", "--goto",
				fmt.Sprintf("%s:%d", wslPath(store.ConfigPath()), line))
		} else {
			editorCmd = exec.Command(editor, fmt.Sprintf("+%d", line), store.ConfigPath())
		}
		return m, tea.ExecProcess(editorCmd, func(err error) tea.Msg {
			return editorDoneMsg{}
		})

	case modalNewSession, modalResumeSession:
		name := val
		if m.allSessionNames()[name] {
			m.setStatus(fmt.Sprintf("session %q already exists — names must be unique", name))
			return m, nil
		}
		m.modal.pendingName = name
		m.modal.step = 1
		m.modal.input.Blur()
		return m, nil

	case modalNewEditorSession:
		name := val
		if m.allSessionNames()[name] {
			m.setStatus(fmt.Sprintf("session %q already exists — names must be unique", name))
			return m, nil
		}
		var proj *store.Project
		for gi := range m.config.Groups {
			if m.config.Groups[gi].Name == m.modal.targetGroup {
				for pi := range m.config.Groups[gi].Projects {
					if m.config.Groups[gi].Projects[pi].Name == m.modal.targetProject {
						proj = &m.config.Groups[gi].Projects[pi]
					}
				}
			}
		}
		if proj == nil {
			m.setStatus("project not found")
			m.modal.mode = modalNone
			return m, nil
		}
		kind := m.modal.targetKind
		var startErr error
		switch kind {
		case "lazygit":
			if m.config.UI.GitClient == "github-desktop" {
				m.setStatus("GitHub Desktop doesn't run inside tmux — use G to open it instead")
				m.modal.mode = modalNone
				m.modal.input.Blur()
				return m, nil
			}
			startErr = tmux.NewToolSession(name, proj.PrimaryRepo(), "lazygit")
		case "terminal":
			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "sh"
			}
			startErr = tmux.NewToolSession(name, proj.PrimaryRepo(), shell)
		default: // "editor"
			editor := resolveEditor(m.config.UI.Editor)
			if isVSCode(editor) {
				m.setStatus("VS Code doesn't run inside tmux — use v to open the folder instead")
				m.modal.mode = modalNone
				m.modal.input.Blur()
				return m, nil
			}
			startErr = tmux.NewToolSession(name, proj.PrimaryRepo(), editor, tmux.ExpandPath(proj.PrimaryRepo()))
		}
		if startErr != nil {
			m.setStatus(fmt.Sprintf("error: %v", startErr))
			m.modal.mode = modalNone
			m.modal.input.Blur()
			return m, nil
		}
		m.config.AddSession(m.modal.targetGroup, m.modal.targetProject, store.Session{Name: name, Kind: kind})
		store.Save(m.config)
		m.modal.mode = modalNone
		m.modal.input.Blur()
		m.rebuildRows()
		for i, r := range m.rows {
			if r.typ == rowTypeSession && r.label == name {
				m.cursor = i
				break
			}
		}
		return m, switchOrAttach(name)
	}

	return m, nil
}

func (m Model) todoProject(t store.Todo) *store.Project {
	if t.GroupName == "" || t.ProjectName == "" {
		return nil
	}
	for gi := range m.config.Groups {
		if m.config.Groups[gi].Name == t.GroupName {
			for pi := range m.config.Groups[gi].Projects {
				if m.config.Groups[gi].Projects[pi].Name == t.ProjectName {
					return &m.config.Groups[gi].Projects[pi]
				}
			}
		}
	}
	return nil
}

func (m Model) openTodoInEditor(t store.Todo) (tea.Model, tea.Cmd) {
	proj := m.todoProject(t)
	if proj == nil {
		m.setStatus("no project linked — run agent first to link a project")
		return m, nil
	}
	// Temporarily point the cursor at a fake row so openInEditor can read the project.
	// Simpler: just duplicate the open logic inline.
	path := tmux.ExpandPath(proj.PrimaryRepo())
	editor := resolveEditor(m.config.UI.Editor)
	var cmd *exec.Cmd
	if isVSCode(editor) {
		cmd = exec.Command(editor, "--new-window", wslPath(path))
	} else {
		cmd = exec.Command(editor, path)
	}
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{} })
}

func (m Model) openTodoInGitClient(t store.Todo) (tea.Model, tea.Cmd) {
	proj := m.todoProject(t)
	if proj == nil {
		m.setStatus("no project linked — run agent first to link a project")
		return m, nil
	}
	path := tmux.ExpandPath(proj.PrimaryRepo())
	client := m.config.UI.GitClient
	if client == "" {
		client = "lazygit"
	}
	if client == "github-desktop" {
		var cmd *exec.Cmd
		if runtime.GOOS == "darwin" {
			cmd = exec.Command("open", "-a", "GitHub Desktop", path)
		} else {
			cmd = exec.Command("github", path)
		}
		return m, func() tea.Msg {
			if err := cmd.Run(); err != nil {
				return popupErrMsg("could not open GitHub Desktop: " + err.Error())
			}
			return nil
		}
	}
	cmd := exec.Command(client, "-p", path)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{} })
}

func (m Model) openTodoInTerminal(t store.Todo) (tea.Model, tea.Cmd) {
	proj := m.todoProject(t)
	if proj == nil {
		m.setStatus("no project linked — run agent first to link a project")
		return m, nil
	}
	if os.Getenv("TMUX") == "" {
		m.setStatus("t requires claudster to be running inside tmux")
		return m, nil
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}
	expanded := tmux.ExpandPath(proj.PrimaryRepo())
	return m, func() tea.Msg {
		out, err := exec.Command("tmux", "display-popup",
			"-E", "-d", expanded,
			"-T", " ctrl-d to close ",
			"-w", "80%", "-h", "80%",
			shell,
		).CombinedOutput()
		if err != nil {
			return popupErrMsg(string(out) + ": " + err.Error())
		}
		return nil
	}
}

func (m Model) quickSpawnTodo(todo store.Todo) (tea.Model, tea.Cmd) {
	var proj *store.Project
	for gi := range m.config.Groups {
		if m.config.Groups[gi].Name == todo.GroupName {
			for pi := range m.config.Groups[gi].Projects {
				if m.config.Groups[gi].Projects[pi].Name == todo.ProjectName {
					proj = &m.config.Groups[gi].Projects[pi]
				}
			}
		}
	}
	if proj == nil {
		m.setStatus("project not found — use n to set it up")
		return m, nil
	}

	name := uniqueSessionName(todoSessionName(&todo), m.allSessionNames())
	prompt := todoPrompt(&todo)

	if err := tmux.NewSessionWithPrompt(name, proj.PrimaryRepo(), proj.AdditionalRepos(), false, prompt); err != nil {
		m.setStatus(fmt.Sprintf("error: %v", err))
		return m, nil
	}

	for i, t := range m.todos.Todos {
		if t.ID == todo.ID {
			m.todos.Todos[i].SessionName = name
			m.todos.Todos[i].Status = store.StatusInProgress
			break
		}
	}
	store.SaveTodos(m.todos)

	m.config.AddSession(todo.GroupName, todo.ProjectName, store.Session{Name: name})
	store.Save(m.config)
	m.rebuildRows()
	m.setStatus(fmt.Sprintf("started %s", name))
	return m, nil
}

func uniqueSessionName(base string, existing map[string]bool) string {
	if base == "" {
		base = "todo"
	}
	if !existing[base] {
		return base
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s-%d", base, i)
		if !existing[name] {
			return name
		}
	}
}

func resolveProjectInput(val string, cfg store.Config) (group, proj string) {
	parts := strings.SplitN(val, "/", 2)
	if len(parts) == 2 {
		g, p := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		for _, grp := range cfg.Groups {
			if strings.EqualFold(grp.Name, g) {
				for _, pr := range grp.Projects {
					if strings.EqualFold(pr.Name, p) {
						return grp.Name, pr.Name
					}
				}
			}
		}
	}
	// Fall back: match project name alone
	for _, grp := range cfg.Groups {
		for _, pr := range grp.Projects {
			if strings.EqualFold(pr.Name, val) {
				return grp.Name, pr.Name
			}
		}
	}
	return "", ""
}

func todoSessionName(t *store.Todo) string {
	if t == nil {
		return ""
	}
	if t.JiraKey != "" {
		return strings.ToLower(t.JiraKey)
	}
	s := strings.ToLower(t.Title)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		if r == ' ' || r == '-' {
			return '-'
		}
		return -1
	}, s)
	if len(s) > 24 {
		s = s[:24]
	}
	return strings.Trim(s, "-")
}

func todoPrompt(t *store.Todo) string {
	if t == nil {
		return ""
	}
	var b strings.Builder
	if t.JiraKey != "" {
		b.WriteString(fmt.Sprintf("[%s] %s", t.JiraKey, t.Title))
	} else {
		b.WriteString(t.Title)
	}
	if t.Description != "" {
		b.WriteString("\n\n")
		b.WriteString(t.Description)
	}
	return b.String()
}

func (m Model) commitTodoSession(dangerous bool) (tea.Model, tea.Cmd) {
	name := m.modal.pendingName
	todo := m.modal.pendingTodo
	m.modal.mode = modalNone
	m.modal.step = 0
	m.modal.pendingName = ""
	m.modal.pendingTodo = nil

	var proj *store.Project
	for gi := range m.config.Groups {
		if m.config.Groups[gi].Name == m.modal.targetGroup {
			for pi := range m.config.Groups[gi].Projects {
				if m.config.Groups[gi].Projects[pi].Name == m.modal.targetProject {
					proj = &m.config.Groups[gi].Projects[pi]
				}
			}
		}
	}
	if proj == nil {
		m.setStatus("project not found")
		return m, nil
	}

	prompt := todoPrompt(todo)
	if err := tmux.NewSessionWithPrompt(name, proj.PrimaryRepo(), proj.AdditionalRepos(), dangerous, prompt); err != nil {
		m.setStatus(fmt.Sprintf("error: %v", err))
		return m, nil
	}

	// Link session name, project, and status back to the todo.
	if todo != nil {
		for i, t := range m.todos.Todos {
			if t.ID == todo.ID {
				m.todos.Todos[i].SessionName = name
				m.todos.Todos[i].GroupName = m.modal.targetGroup
				m.todos.Todos[i].ProjectName = m.modal.targetProject
				m.todos.Todos[i].Status = store.StatusInProgress
				break
			}
		}
		store.SaveTodos(m.todos)
	}

	m.config.AddSession(m.modal.targetGroup, m.modal.targetProject, store.Session{Name: name})
	store.Save(m.config)
	m.rebuildRows()
	for i, r := range m.rows {
		if r.typ == rowTypeSession && r.label == name {
			m.cursor = i
			break
		}
	}
	m.sidebarMode = sidebarModeSessions
	return m, switchOrAttach(name)
}

// commitSession is called after the dangerous-mode confirmation step. It
// creates the session and switches to it.
func (m Model) commitSession(dangerous bool) (tea.Model, tea.Cmd) {
	name := m.modal.pendingName
	mode := m.modal.mode
	m.modal.mode = modalNone
	m.modal.step = 0
	m.modal.pendingName = ""

	var proj *store.Project
	for gi := range m.config.Groups {
		if m.config.Groups[gi].Name == m.modal.targetGroup {
			for pi := range m.config.Groups[gi].Projects {
				if m.config.Groups[gi].Projects[pi].Name == m.modal.targetProject {
					proj = &m.config.Groups[gi].Projects[pi]
				}
			}
		}
	}
	if proj == nil {
		m.setStatus("project not found")
		return m, nil
	}

	var err error
	if mode == modalResumeSession {
		err = tmux.NewResumeSession(name, proj.PrimaryRepo(), proj.AdditionalRepos(), dangerous)
	} else {
		err = tmux.NewSession(name, proj.PrimaryRepo(), proj.AdditionalRepos(), dangerous)
	}
	if err != nil {
		m.setStatus(fmt.Sprintf("error: %v", err))
		return m, nil
	}

	m.config.AddSession(m.modal.targetGroup, m.modal.targetProject, store.Session{Name: name})
	store.Save(m.config)
	m.rebuildRows()
	for i, r := range m.rows {
		if r.typ == rowTypeSession && r.label == name {
			m.cursor = i
			break
		}
	}
	return m, switchOrAttach(name)
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		return "loading..."
	}
	if m.modal.mode != modalNone {
		return renderModal(m)
	}
	sidebar := renderSidebar(m)
	right := renderRightPanel(m)
	main := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, right)
	view := lipgloss.JoinVertical(lipgloss.Left, main, renderHelpBar(m))

	if len(m.toasts) > 0 {
		panel := renderAllToasts(m)
		panelH := strings.Count(panel, "\n") + 1
		// Position flush to the right edge, above the help bar.
		x := m.width - toastOuterW
		y := m.height - 1 - panelH
		if x >= 0 && y >= 0 {
			view = overlayStrings(view, panel, x, y)
		}
	}

	return view
}

// ── shared helpers ────────────────────────────────────────────────────────────

func gitClientLabel(configured string) string {
	if configured == "github-desktop" {
		return "GitHub Desktop"
	}
	return "lazygit"
}

func existingGroupHint(cfg store.Config) string {
	var names []string
	for _, g := range cfg.Groups {
		names = append(names, g.Name)
	}
	if len(names) == 0 {
		return "e.g. work"
	}
	return strings.Join(names, ", ")
}

func parseRepos(val string) []string {
	parts := strings.Split(val, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

func renderHelpBar(m Model) string {
	status := ""
	if m.status != "" && time.Now().Before(m.statusExp) {
		status = "  " + ErrorStyle.Render(m.status)
	}

	bindings := []struct{ key, desc string }{
		{"enter", "attach"},
		{"n", "new session"},
		{"r", "resume"},
		{"t/T", "terminal"},
		{"v/V", "editor"},
		{"G", gitClientLabel(m.config.UI.GitClient)},
		{"d", "delete"},
		{"N", "new project"},
		{"tab", "todos"},
		{"?", "help"},
		{"q", "quit"},
	}
	var parts []string
	for _, b := range bindings {
		parts = append(parts, HelpKey.Render(b.key)+HelpSep.Render(":")+HelpDesc.Render(b.desc))
	}
	if m.dangerousMode {
		parts = append(parts, ErrorStyle.Render("p")+HelpSep.Render(":")+ErrorStyle.Render("bypass ON"))
	} else {
		parts = append(parts, HelpKey.Render("p")+HelpSep.Render(":")+HelpDesc.Render("bypass off"))
	}
	line := strings.Join(parts, HelpSep.Render("  "))
	return StatusBar.Width(m.width).Render(line + status)
}
