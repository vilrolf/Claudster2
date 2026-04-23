package tmux

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Status int

const (
	StatusIdle    Status = iota
	StatusWorking        // content is actively changing
	StatusDone           // was working, now stopped
	StatusDead           // session no longer exists
)

type State struct {
	Status     Status
	FinishedAt *time.Time
}

type Monitor struct {
	mu        sync.RWMutex
	states    map[string]*mstate
	doneTimes map[string]time.Time // persisted across restarts
	liveSet   map[string]bool      // updated each Poll via list-sessions (one subprocess)
}

type mstate struct {
	status      Status
	finishedAt  *time.Time
	lastContent string
	lastChanged time.Time
}

func persistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claudster.sessions.json")
}

func loadDoneTimes() map[string]time.Time {
	data, err := os.ReadFile(persistPath())
	if err != nil {
		return map[string]time.Time{}
	}
	var m map[string]time.Time
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]time.Time{}
	}
	return m
}

func saveDoneTimes(m map[string]time.Time) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	os.WriteFile(persistPath(), data, 0644)
}

func NewMonitor() *Monitor {
	return &Monitor{
		states:    make(map[string]*mstate),
		doneTimes: loadDoneTimes(),
		liveSet:   make(map[string]bool),
	}
}

// Exists reports whether a tmux session is currently alive, using the cached
// result from the last Poll. Pure map lookup — no subprocess.
func (m *Monitor) Exists(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.liveSet[name]
}

// Poll updates the state for all given session names.
func (m *Monitor) Poll(sessions []string) {
	// One subprocess to get all live sessions — replaces N has-session calls.
	liveSet := make(map[string]bool)
	if out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output(); err == nil {
		for _, name := range strings.Fields(string(out)) {
			liveSet[name] = true
		}
	}

	// Only poll pane content for sessions that are actually alive.
	alive := make(map[string]bool, len(sessions))
	for _, name := range sessions {
		if liveSet[name] {
			alive[name] = true
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.liveSet = liveSet

	// Mark removed sessions as dead
	for name, s := range m.states {
		if !alive[name] {
			s.status = StatusDead
		}
	}

	persistDirty := false

	for _, name := range sessions {
		content := capturePane(name)
		s, ok := m.states[name]
		if !ok {
			// Zero lastChanged so time.Since(zero) is huge → isChanging = false
			s = &mstate{lastChanged: time.Time{}, lastContent: content}
			// Restore persisted Done state if we have one
			if t, hasDone := m.doneTimes[name]; hasDone {
				s.status = StatusDone
				s.finishedAt = &t
			}
			m.states[name] = s
		}

		if content != s.lastContent {
			s.lastContent = content
			s.lastChanged = time.Now()
		}

		wasWorking := s.status == StatusWorking
		isChanging := time.Since(s.lastChanged) < 2*time.Second

		switch {
		case isChanging:
			// Session became active again — clear any persisted done time
			if s.status == StatusDone {
				delete(m.doneTimes, name)
				persistDirty = true
			}
			s.status = StatusWorking
			s.finishedAt = nil
		case wasWorking:
			now := time.Now()
			s.status = StatusDone
			s.finishedAt = &now
			m.doneTimes[name] = now
			persistDirty = true
		case s.status != StatusDone:
			s.status = StatusIdle
		}
	}

	if persistDirty {
		saveDoneTimes(m.doneTimes)
	}
}

func (m *Monitor) Get(name string) State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.states[name]
	if !ok {
		return State{Status: StatusIdle}
	}
	return State{Status: s.status, FinishedAt: s.finishedAt}
}

func capturePane(session string) string {
	out, err := exec.Command("tmux", "capture-pane", "-t", session, "-p").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// CapturePaneOutput returns the last n lines of a session's active pane,
// including ANSI color/style escape sequences.
func CapturePaneOutput(session string, lines int) string {
	out, err := exec.Command("tmux", "capture-pane", "-t", session, "-p", "-e",
		"-S", fmt.Sprintf("-%d", lines)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}
