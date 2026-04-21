package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"

	"claudster/ui"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "update":
			runUpdate()
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
