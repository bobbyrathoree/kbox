package components

import "github.com/charmbracelet/lipgloss"

// Colors
var (
	ColorPrimary   = lipgloss.Color("86")  // Cyan
	ColorSecondary = lipgloss.Color("243") // Gray
	ColorSuccess   = lipgloss.Color("82")  // Green
	ColorWarning   = lipgloss.Color("214") // Orange
	ColorError     = lipgloss.Color("196") // Red
	ColorMuted     = lipgloss.Color("240") // Dark gray
	ColorHighlight = lipgloss.Color("212") // Pink/magenta
)

// Base styles
var (
	// Title bar
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary)

	// Section headers
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			MarginBottom(1)

	// Panel border
	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorSecondary).
			Padding(0, 1)

	// Focused panel border
	FocusedPanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorPrimary).
				Padding(0, 1)

	// Status indicators
	StatusRunning = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			SetString("●")

	StatusPending = lipgloss.NewStyle().
			Foreground(ColorWarning).
			SetString("○")

	StatusError = lipgloss.NewStyle().
			Foreground(ColorError).
			SetString("●")

	StatusUnknown = lipgloss.NewStyle().
			Foreground(ColorMuted).
			SetString("○")

	// Labels
	LabelStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary)

	ValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))

	// Help bar
	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary)

	// Log line styles
	LogTimestampStyle = lipgloss.NewStyle().
				Foreground(ColorMuted)

	LogSourceStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary)

	LogEventStyle = lipgloss.NewStyle().
			Foreground(ColorWarning)

	// Error message
	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError)

	// Sparkline characters (from low to high)
	SparklineChars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
)

// StatusIcon returns the appropriate status indicator
func StatusIcon(phase string, ready bool) string {
	switch phase {
	case "Running":
		if ready {
			return StatusRunning.String()
		}
		return StatusPending.Render("○")
	case "Pending":
		return StatusPending.Render("○")
	case "Failed", "Error":
		return StatusError.String()
	case "Succeeded":
		return StatusRunning.String()
	default:
		return StatusUnknown.Render("○")
	}
}

// Sparkline generates an ASCII sparkline from values
func Sparkline(values []float64, width int) string {
	if len(values) == 0 {
		return ""
	}

	// Find min and max
	min, max := values[0], values[0]
	for _, v := range values {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}

	// Normalize and render
	result := make([]rune, 0, width)
	step := len(values) / width
	if step < 1 {
		step = 1
	}

	for i := 0; i < width && i*step < len(values); i++ {
		v := values[i*step]
		var idx int
		if max > min {
			idx = int((v - min) / (max - min) * float64(len(SparklineChars)-1))
		}
		if idx >= len(SparklineChars) {
			idx = len(SparklineChars) - 1
		}
		result = append(result, SparklineChars[idx])
	}

	return string(result)
}

// TruncateWithEllipsis truncates a string and adds ellipsis if needed
func TruncateWithEllipsis(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// FormatBytes formats bytes into human-readable string
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return lipgloss.NewStyle().Render("0B")
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"Ki", "Mi", "Gi", "Ti"}
	return lipgloss.NewStyle().Render(
		lipgloss.NewStyle().Render(
			string(rune('0'+bytes/div)) + units[exp],
		),
	)
}
