package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"claudster/ui"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "update":
			runUpdate()
			return
		case "setup":
			runSetup()
			return
		case "version", "--version", "-v":
			fmt.Println(ui.Version)
			return
		}
	}

	m := ui.New()
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseAllMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runSetup() {
	tmuxConf := os.ExpandEnv("$HOME/.tmux.conf")
	line := "set -g mouse on"

	data, _ := os.ReadFile(tmuxConf)
	if len(data) > 0 {
		for _, l := range splitLines(string(data)) {
			if strings.TrimSpace(l) == line {
				fmt.Println("~/.tmux.conf already has 'set -g mouse on' — nothing to do.")
				return
			}
		}
	}

	f, err := os.OpenFile(tmuxConf, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not write ~/.tmux.conf: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	prefix := ""
	if len(data) > 0 && data[len(data)-1] != '\n' {
		prefix = "\n"
	}
	if _, err := fmt.Fprintf(f, "%s# added by claudster setup\n%s\n", prefix, line); err != nil {
		fmt.Fprintf(os.Stderr, "write failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Added 'set -g mouse on' to ~/.tmux.conf.")
	fmt.Println("Reload tmux config with: tmux source ~/.tmux.conf")
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func runUpdate() {
	var osName, arch string
	switch runtime.GOOS {
	case "darwin":
		osName = "darwin"
	case "linux":
		osName = "linux"
	default:
		fmt.Fprintf(os.Stderr, "auto-update not supported on %s — download manually from https://github.com/jonagull/claudster/releases/latest\n", runtime.GOOS)
		os.Exit(1)
	}
	switch runtime.GOARCH {
	case "arm64":
		arch = "arm64"
	default:
		arch = "amd64"
	}

	url := fmt.Sprintf("https://github.com/jonagull/claudster/releases/latest/download/claudster-%s-%s", osName, arch)
	fmt.Printf("Downloading latest claudster (%s-%s)...\n", osName, arch)

	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "download failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "download failed: HTTP %d\n", resp.StatusCode)
		os.Exit(1)
	}

	self, err := exec.LookPath(os.Args[0])
	if err != nil {
		self = os.Args[0]
	}

	// Write to a temp file alongside the binary, then rename (atomic on same fs).
	tmp := self + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot write update: %v\n", err)
		os.Exit(1)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		fmt.Fprintf(os.Stderr, "write failed: %v\n", err)
		os.Exit(1)
	}
	f.Close()

	if err := os.Rename(tmp, self); err != nil {
		// Rename may fail if binary is in a root-owned dir (e.g. /usr/local/bin).
		// Fall back to suggesting sudo.
		os.Remove(tmp)
		fmt.Fprintf(os.Stderr, "could not replace binary (permission denied) — try:\n  sudo claudster update\n")
		os.Exit(1)
	}

	fmt.Println("Updated successfully. Run 'claudster version' to confirm.")
}
