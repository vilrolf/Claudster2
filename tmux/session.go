package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func ListSessions() ([]string, error) {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		// tmux returns error if no sessions exist
		return nil, nil
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}

func NewSession(name, path string, additionalRepos []string, dangerous bool) error {
	args := []string{"new-session", "-d", "-s", name, "-c", ExpandPath(path), "claude"}
	for _, r := range additionalRepos {
		args = append(args, "--add-dir", ExpandPath(r))
	}
	if dangerous {
		args = append(args, "--dangerously-skip-permissions")
	}
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

// NewResumeSession starts a tmux session running `claude --resume`, which
// presents an interactive picker so the user can choose which conversation
// to continue.
func NewResumeSession(name, path string, additionalRepos []string, dangerous bool) error {
	args := []string{"new-session", "-d", "-s", name, "-c", ExpandPath(path), "claude", "--resume"}
	for _, r := range additionalRepos {
		args = append(args, "--add-dir", ExpandPath(r))
	}
	if dangerous {
		args = append(args, "--dangerously-skip-permissions")
	}
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

// NewSessionWithPrompt starts a Claude session with an initial prompt pre-filled.
func NewSessionWithPrompt(name, path string, additionalRepos []string, dangerous bool, prompt string) error {
	args := []string{"new-session", "-d", "-s", name, "-c", ExpandPath(path), "claude"}
	for _, r := range additionalRepos {
		args = append(args, "--add-dir", ExpandPath(r))
	}
	if dangerous {
		args = append(args, "--dangerously-skip-permissions")
	}
	if prompt != "" {
		args = append(args, prompt)
	}
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

// NewToolSession starts a tmux session running an arbitrary command.
// programArgs are appended after program (e.g. pass the path for editors;
// leave empty for tools like lazygit that use the working directory).
func NewToolSession(name, path, program string, programArgs ...string) error {
	expanded := ExpandPath(path)
	args := []string{"new-session", "-d", "-s", name, "-c", expanded, program}
	args = append(args, programArgs...)
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

// RestartSession kills the claude process running in the pane (without typing
// into it), then resumes the specific conversation by ID with the given flags.
// projectPath is the primary repo path used to locate the conversation in
// ~/.claude/projects/ — it is only used for the ID lookup and not passed here
// directly; callers should pass the resolved convoID (empty = fall back to
// `claude --resume`).
func RestartSession(name, convoID string, dangerous bool) {
	// Get the PID of the shell that owns this pane.
	out, err := exec.Command("tmux", "display-message", "-t", name, "-p", "#{pane_pid}").Output()
	if err != nil {
		return
	}
	shellPID := strings.TrimSpace(string(out))

	// Kill all direct children of the shell (i.e. the claude process).
	exec.Command("sh", "-c", "kill $(pgrep -P "+shellPID+") 2>/dev/null").Run()
	time.Sleep(500 * time.Millisecond)

	// Build the resume command with the specific conversation ID so we don't
	// accidentally resume a different conversation.
	var cmd string
	if convoID != "" {
		cmd = "claude resume " + convoID
	} else {
		cmd = "claude --resume"
	}
	if dangerous {
		cmd += " --dangerously-skip-permissions"
	}
	exec.Command("tmux", "send-keys", "-t", name, cmd, "Enter").Run()
}

func KillSession(name string) error {
	return exec.Command("tmux", "kill-session", "-t", name).Run()
}

func SwitchTo(name string) error {
	return exec.Command("tmux", "switch-client", "-t", name).Run()
}

func SessionExists(name string) bool {
	err := exec.Command("tmux", "has-session", "-t", name).Run()
	return err == nil
}
