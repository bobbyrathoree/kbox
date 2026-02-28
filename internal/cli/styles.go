package cli

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Step header: bold cyan "▸ Building image..."
	stepStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	// Success: green "✓"
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	// Warning: yellow "⚠"
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	// Error: red "✗"
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	// Dim: for secondary info
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	// Bold: for emphasis
	boldStyle = lipgloss.NewStyle().Bold(true)
)

// step prints a styled step header like "▸ Building image..."
func step(msg string) {
	fmt.Println(stepStyle.Render("▸ " + msg))
}

// success prints a styled success line like "  ✓ Built in 3.2s"
func success(msg string) {
	fmt.Println(successStyle.Render("  ✓ ") + msg)
}

// warn prints a styled warning
func warn(msg string) {
	fmt.Fprintln(os.Stderr, warnStyle.Render("  ⚠ ")+msg)
}

// dimInfo prints secondary info
func dimInfo(msg string) {
	fmt.Println(dimStyle.Render("  " + msg))
}

// diagnosisBox prints a styled red box for deploy failure diagnosis
func diagnosisBox(content string) {
	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("1")).
		Padding(0, 1).
		Width(72)
	fmt.Fprintln(os.Stderr, style.Render(content))
}
