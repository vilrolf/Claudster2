package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"claudster/ui"
)

func main() {
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
