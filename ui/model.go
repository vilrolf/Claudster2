package ui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"claudster/metrics"
	"claudster/store"
	"claudster/tmux"
)

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// ── row types ─────────────────────────────────────────────────────────────────

type rowType int

const (
	rowTypeOverview rowType = iota
	rowTypeGroup
	rowTypeProject
	rowTypeSession
)

type sidebarRow struct {
	typ        rowType
	label      string
	groupIdx   int
	projectIdx int
	sessionIdx int
	yPos       int // terminal Y for mouse hit-testing
}

// ── modal ─────────────────────────────────────────────────────────────────────

type modalMode int

const (
	modalNone             modalMode = iota
	modalNewProject                 // N
	modalNewSession                 // n
	modalResumeSession              // r
	modalNewEditorSession           // V / G / T
	modalConfirmDelete              // d
	modalHelp                       // ?
)

// modalNewProject only needs one step (group name); the rest is done in $EDITOR.
// modalNewSession only needs one step (session name).

type modalState struct {
	mode          modalMode
	targetGroup   string
	targetProject string
	targetKind    string // for modalNewEditorSession: "editor" or "lazygit"
	input         textinput.Model
	completions   []string // tab-cycle candidates (group names)
	compIdx       int
	step          int    // 0 = name input, 1 = dangerous mode confirm
	pendingName   string // session name held between steps
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

	rows     []sidebarRow
	cursor   int
	expanded map[string]bool
	yToRow   map[int]int

	modal modalState

	spinFrame    int
	spinTick     int
	claudeStats  metrics.Stats
	dangerousMode bool

	sidebarW int
	dashW    int
	dashH    int
	width    int
	height   int

	status    string
	statusExp time.Time

	toasts         []toast
	tmuxBoundCount int // how many Alt+N tmux keys are currently bound
}

type tickMsg time.Time
type attachDoneMsg struct{ err error }
type editorDoneMsg struct{}
type metricsMsg metrics.Stats
type popupErrMsg string

func New() Model {
	cfg, cfgErr := store.Load()
	m := Model{
		config:   cfg,
		monitor:  tmux.NewMonitor(),
		expanded: make(map[string]bool),
		yToRow:   make(map[int]int),
		modal:    newModalState(),
	}
	if cfgErr != nil {
		m.configErr = cfgErr.Error()
	}
	for gi, g := range cfg.Groups {
		for pi := range g.Projects {
			m.expanded[expandKey(gi, pi)] = true
		}
	}
	m.rebuildRows()
	m.pollMonitor()
	if os.Getenv("TMUX") != "" {
		exec.Command("tmux", "set-option", "-g", "mouse", "on").Run()
	}
	return m
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

	m.rows = rows
	m.yToRow = make(map[int]int, len(rows))
	for i, r := range rows {
		m.yToRow[r.yPos] = i
	}
	m.clampCursor()
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
	for m.cursor < len(m.rows) && m.rows[m.cursor].typ == rowTypeGroup {
		m.cursor++
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
		for m.cursor > 0 && m.rows[m.cursor].typ == rowTypeGroup {
			m.cursor--
		}
	}
	// If still negative (only group rows exist), leave at 0 — View guards against this
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *Model) pollMonitor() {
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
	// Snapshot Claude session statuses before polling to detect Working→Done transitions.
	prev := make(map[string]tmux.Status, len(claudeNames))
	for _, n := range claudeNames {
		prev[n] = m.monitor.Get(n).Status
	}
	m.monitor.Poll(allNames)
	for _, n := range claudeNames {
		if prev[n] == tmux.StatusWorking && m.monitor.Get(n).Status == tmux.StatusDone {
			m.addToast(n)
		}
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
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Init() tea.Cmd {
	return tick()
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
		m.pollMonitor()
		m.tickToasts()
		cmds := []tea.Cmd{tick()}
		if m.spinTick == 1 || m.spinTick%20 == 0 { // first tick, then every ~10 s
			cmds = append(cmds, func() tea.Msg {
				return metricsMsg(metrics.Collect())
			})
		}
		return m, tea.Batch(cmds...)

	case metricsMsg:
		m.claudeStats = metrics.Stats(msg)

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
			m.moveUp()
			return m, nil
		case tea.MouseButtonWheelDown:
			m.moveDown()
			return m, nil
		}

	case tea.KeyMsg:
		if m.modal.mode != modalNone {
			return m.handleModalKey(msg)
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

	case "d":
		return m.startConfirmDelete(), nil

	case "p":
		m.dangerousMode = !m.dangerousMode

	case "P":
		return m.restartCurrentSession()

	case "v":
		return m.openInEditor()

	case "V":
		return m.startNewToolSession("editor"), nil

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

func (m *Model) moveDown() {
	next := m.cursor + 1
	for next < len(m.rows) && m.rows[next].typ == rowTypeGroup {
		next++
	}
	if next < len(m.rows) {
		m.cursor = next
	}
}

func (m *Model) moveUp() {
	prev := m.cursor - 1
	for prev >= 0 && m.rows[prev].typ == rowTypeGroup {
		prev--
	}
	if prev >= 0 {
		m.cursor = prev
	}
}

func (m *Model) toggleExpand() {
	if m.cursor >= len(m.rows) {
		return
	}
	row := m.rows[m.cursor]
	var key string
	switch row.typ {
	case rowTypeProject:
		key = expandKey(row.groupIdx, row.projectIdx)
	case rowTypeSession:
		key = expandKey(row.groupIdx, row.projectIdx)
	default:
		return
	}
	m.expanded[key] = !m.expanded[key]
	m.rebuildRows()
}

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.rows) {
		return m, nil
	}
	row := m.rows[m.cursor]

	switch row.typ {
	case rowTypeProject:
		m.toggleExpand()
		return m, nil

	case rowTypeSession:
		if !tmux.SessionExists(row.label) {
			// Start then attach
			proj := &m.config.Groups[row.groupIdx].Projects[row.projectIdx]
			if err := tmux.NewSession(row.label, proj.PrimaryRepo(), proj.AdditionalRepos(), m.dangerousMode); err != nil {
				m.setStatus(fmt.Sprintf("error starting session: %v", err))
				return m, nil
			}
		}
		return m, switchOrAttach(row.label)
	}

	return m, nil
}

// switchOrAttach switches to the named session. When running inside tmux it
// uses switch-client (claudster stays alive in its own window). Outside tmux
// it falls back to attach-session via ExecProcess.
func switchOrAttach(name string) tea.Cmd {
	if os.Getenv("TMUX") != "" {
		return func() tea.Msg {
			exec.Command("tmux", "bind-key", "-n", "C-q", "switch-client", "-l").Run()
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
	if m.rows[m.cursor].typ != rowTypeSession {
		return m
	}
	m.modal.mode = modalConfirmDelete
	return m
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
	if row.typ != rowTypeSession || !tmux.SessionExists(row.label) {
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
		case rowTypeGroup:
			// no-op
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
				if tmux.SessionExists(s.Name) {
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
	// Step 1: dangerous mode confirmation for new/resume session.
	if m.modal.step == 1 {
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
			m.deleteSelected()
			return m, nil
		}
	case "tab":
		if m.modal.mode == modalNewProject {
			m.cycleGroupCompletion()
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

func (m Model) handleModalEnter() (tea.Model, tea.Cmd) {
	val := strings.TrimSpace(m.modal.input.Value())
	if val == "" {
		return m, nil
	}

	switch m.modal.mode {
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
