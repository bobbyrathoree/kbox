package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bobbyrathoree/kbox/internal/debug"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/tui/components"
)

const (
	refreshInterval = 2 * time.Second
	maxLogLines     = 1000
)

// Focus panels
const (
	focusPods = iota
	focusLogs
)

// Model is the main Bubbletea model for the dashboard
type Model struct {
	// K8s connection
	client    *k8s.Client
	appName   string
	namespace string
	context   string

	// Data
	status    *debug.AppStatus
	logs      []debug.LogLine
	pods      []debug.PodInfo
	cpuHist   []float64
	memHist   []float64

	// UI state
	focused       int
	fullscreen    bool
	width, height int
	lastError     error
	showHelp      bool

	// Components
	logsViewport viewport.Model

	// Channels for log streaming
	logChan    chan debug.LogLine
	cancelLogs context.CancelFunc
}

// Message types
type statusMsg *debug.AppStatus
type podsMsg []debug.PodInfo
type logMsg debug.LogLine
type tickMsg time.Time
type errMsg error

// NewDashboard creates a new dashboard model
func NewDashboard(client *k8s.Client, appName, namespace string) Model {
	vp := viewport.New(80, 10)
	vp.SetContent("")

	return Model{
		client:       client,
		appName:      appName,
		namespace:    namespace,
		context:      client.Context,
		logsViewport: vp,
		focused:      focusPods,
		cpuHist:      make([]float64, 0, 30),
		memHist:      make([]float64, 0, 30),
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchStatus(),
		m.startLogStream(),
		m.tick(),
	)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateViewportSize()
		return m, nil

	case statusMsg:
		m.status = msg
		m.lastError = nil
		// Update metrics history
		if msg != nil && msg.Deployment != nil {
			// Simulate CPU/Memory for now (would come from metrics-server)
			// This is placeholder until we add metrics-server integration
			if len(m.cpuHist) >= 30 {
				m.cpuHist = m.cpuHist[1:]
			}
			if len(m.memHist) >= 30 {
				m.memHist = m.memHist[1:]
			}
			// Add some variation for visual effect
			m.cpuHist = append(m.cpuHist, float64(20+len(m.cpuHist)%30))
			m.memHist = append(m.memHist, float64(40+len(m.memHist)%20))
		}
		return m, nil

	case podsMsg:
		m.pods = msg
		return m, nil

	case logMsg:
		m.addLog(debug.LogLine(msg))
		m.updateLogsContent()
		return m, nil

	case tickMsg:
		return m, tea.Batch(
			m.fetchStatus(),
			m.tick(),
		)

	case errMsg:
		m.lastError = msg
		return m, nil
	}

	// Update viewport
	if m.focused == focusLogs {
		var cmd tea.Cmd
		m.logsViewport, cmd = m.logsViewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// handleKeyPress handles keyboard input
func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if m.cancelLogs != nil {
			m.cancelLogs()
		}
		return m, tea.Quit

	case "tab":
		m.focused = (m.focused + 1) % 2
		return m, nil

	case "l":
		m.fullscreen = !m.fullscreen
		m.updateViewportSize()
		return m, nil

	case "r":
		return m, m.restartDeployment()

	case "?":
		m.showHelp = !m.showHelp
		return m, nil

	case "up", "k":
		if m.focused == focusLogs {
			m.logsViewport.LineUp(1)
		}
		return m, nil

	case "down", "j":
		if m.focused == focusLogs {
			m.logsViewport.LineDown(1)
		}
		return m, nil

	case "pgup":
		if m.focused == focusLogs {
			m.logsViewport.ViewUp()
		}
		return m, nil

	case "pgdown":
		if m.focused == focusLogs {
			m.logsViewport.ViewDown()
		}
		return m, nil
	}

	return m, nil
}

// View renders the UI
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.fullscreen {
		return m.renderFullscreenLogs()
	}

	if m.showHelp {
		return m.renderHelp()
	}

	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Top row: Deployment + Metrics
	topRow := m.renderTopRow()
	b.WriteString(topRow)
	b.WriteString("\n")

	// Pods panel
	b.WriteString(m.renderPods())
	b.WriteString("\n")

	// Logs panel
	b.WriteString(m.renderLogs())
	b.WriteString("\n")

	// Help bar
	b.WriteString(m.renderHelpBar())

	return b.String()
}

// renderHeader renders the title bar
func (m Model) renderHeader() string {
	title := components.TitleStyle.Render(m.appName)
	ns := components.LabelStyle.Render(m.namespace)
	ctx := components.LabelStyle.Render(m.context)

	left := fmt.Sprintf("%s │ %s │ %s", title, ns, ctx)

	help := components.HelpKeyStyle.Render("[?]") + " " + components.HelpDescStyle.Render("help")
	right := help

	// Calculate padding
	padding := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if padding < 0 {
		padding = 0
	}

	return left + strings.Repeat(" ", padding) + right
}

// renderTopRow renders deployment status and metrics side by side
func (m Model) renderTopRow() string {
	halfWidth := (m.width - 3) / 2

	// Deployment panel
	depContent := m.renderDeploymentContent()
	depPanel := components.PanelStyle.
		Width(halfWidth).
		Render(components.HeaderStyle.Render("DEPLOYMENT") + "\n" + depContent)

	// Metrics panel
	metricsContent := m.renderMetricsContent()
	metricsPanel := components.PanelStyle.
		Width(halfWidth).
		Render(components.HeaderStyle.Render("METRICS") + "\n" + metricsContent)

	return lipgloss.JoinHorizontal(lipgloss.Top, depPanel, " ", metricsPanel)
}

// renderDeploymentContent renders deployment status
func (m Model) renderDeploymentContent() string {
	if m.status == nil || m.status.Deployment == nil {
		return components.LabelStyle.Render("Loading...")
	}

	d := m.status.Deployment
	var lines []string

	// Status line
	statusIcon := components.StatusIcon("Running", d.ReadyReplicas == d.Replicas)
	statusText := fmt.Sprintf("%s Running (%d/%d ready)", statusIcon, d.ReadyReplicas, d.Replicas)
	lines = append(lines, statusText)

	// Image
	image := components.TruncateWithEllipsis(d.Image, 40)
	lines = append(lines, components.LabelStyle.Render("Image: ")+image)

	// Age
	age := formatDuration(time.Since(d.CreatedAt))
	lines = append(lines, components.LabelStyle.Render("Age: ")+age)

	return strings.Join(lines, "\n")
}

// renderMetricsContent renders CPU/Memory sparklines
func (m Model) renderMetricsContent() string {
	if len(m.cpuHist) == 0 {
		return components.LabelStyle.Render("Gathering metrics...")
	}

	var lines []string

	// CPU sparkline
	cpuSparkline := components.Sparkline(m.cpuHist, 20)
	cpuLast := m.cpuHist[len(m.cpuHist)-1]
	lines = append(lines, fmt.Sprintf("CPU  %s %.0f%%", cpuSparkline, cpuLast))

	// Memory sparkline
	memSparkline := components.Sparkline(m.memHist, 20)
	memLast := m.memHist[len(m.memHist)-1]
	lines = append(lines, fmt.Sprintf("Mem  %s %.0fMi", memSparkline, memLast))

	return strings.Join(lines, "\n")
}

// renderPods renders the pods panel
func (m Model) renderPods() string {
	var content strings.Builder

	header := "PODS"
	if m.status != nil {
		header = fmt.Sprintf("PODS (%d)", len(m.status.Pods))
	}
	content.WriteString(components.HeaderStyle.Render(header))
	content.WriteString("\n")

	if m.status == nil || len(m.status.Pods) == 0 {
		content.WriteString(components.LabelStyle.Render("No pods found"))
	} else {
		for _, pod := range m.status.Pods {
			icon := components.StatusIcon(pod.Phase, pod.Ready)
			age := formatDuration(pod.Age)
			line := fmt.Sprintf("%s %-30s %-10s %s",
				icon,
				components.TruncateWithEllipsis(pod.Name, 30),
				pod.Phase,
				age,
			)
			content.WriteString(line)
			content.WriteString("\n")
		}
	}

	style := components.PanelStyle
	if m.focused == focusPods {
		style = components.FocusedPanelStyle
	}

	return style.Width(m.width - 2).Render(content.String())
}

// renderLogs renders the logs panel
func (m Model) renderLogs() string {
	header := "LOGS"
	if len(m.logs) > 0 {
		header = fmt.Sprintf("LOGS (%d lines)", len(m.logs))
	}

	style := components.PanelStyle
	if m.focused == focusLogs {
		style = components.FocusedPanelStyle
	}

	content := components.HeaderStyle.Render(header) + "\n" + m.logsViewport.View()
	return style.Width(m.width - 2).Render(content)
}

// renderFullscreenLogs renders logs in fullscreen mode
func (m Model) renderFullscreenLogs() string {
	header := components.TitleStyle.Render("LOGS - Press 'l' to exit fullscreen")
	m.logsViewport.Width = m.width - 4
	m.logsViewport.Height = m.height - 4
	return header + "\n\n" + m.logsViewport.View() + "\n\n" + m.renderHelpBar()
}

// renderHelp renders the help overlay
func (m Model) renderHelp() string {
	help := `
KEYBOARD SHORTCUTS

  Navigation
  ──────────
  Tab        Switch between pods and logs panes
  ↑/↓ or j/k Scroll logs
  PgUp/PgDn  Page through logs

  Actions
  ───────
  r          Restart deployment
  l          Toggle fullscreen logs

  General
  ───────
  ?          Toggle this help
  q          Quit

Press any key to close help...
`
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(components.ColorPrimary).
		Padding(1, 2).
		Width(50)

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		style.Render(help),
	)
}

// renderHelpBar renders the bottom help bar
func (m Model) renderHelpBar() string {
	items := []string{
		components.HelpKeyStyle.Render("[r]") + " " + components.HelpDescStyle.Render("restart"),
		components.HelpKeyStyle.Render("[l]") + " " + components.HelpDescStyle.Render("fullscreen logs"),
		components.HelpKeyStyle.Render("[Tab]") + " " + components.HelpDescStyle.Render("switch pane"),
		components.HelpKeyStyle.Render("[q]") + " " + components.HelpDescStyle.Render("quit"),
	}

	if m.lastError != nil {
		items = append([]string{components.ErrorStyle.Render("Error: " + m.lastError.Error())}, items...)
	}

	return "  " + strings.Join(items, "  ")
}

// Helper methods

func (m *Model) updateViewportSize() {
	// Calculate available height for logs viewport
	// Header (1) + top row (~5) + pods (~6) + help bar (1) + borders
	usedHeight := 20
	if m.status != nil {
		usedHeight += len(m.status.Pods)
	}
	availableHeight := m.height - usedHeight
	if availableHeight < 5 {
		availableHeight = 5
	}

	if m.fullscreen {
		availableHeight = m.height - 4
	}

	m.logsViewport.Width = m.width - 6
	m.logsViewport.Height = availableHeight
}

func (m *Model) addLog(line debug.LogLine) {
	m.logs = append(m.logs, line)
	if len(m.logs) > maxLogLines {
		m.logs = m.logs[len(m.logs)-maxLogLines:]
	}
}

func (m *Model) updateLogsContent() {
	var content strings.Builder
	for _, line := range m.logs {
		ts := components.LogTimestampStyle.Render(line.Timestamp.Format("15:04:05"))
		src := components.LogSourceStyle.Render(fmt.Sprintf("[%-12s]", line.Source))
		if line.IsEvent {
			src = components.LogEventStyle.Render(fmt.Sprintf("[%-12s]", line.Source))
		}
		content.WriteString(fmt.Sprintf("%s %s %s\n", ts, src, line.Message))
	}
	m.logsViewport.SetContent(content.String())
	m.logsViewport.GotoBottom()
}

// Commands

func (m Model) fetchStatus() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		status, err := debug.GetAppStatus(ctx, m.client.Clientset, m.namespace, m.appName)
		if err != nil {
			return errMsg(err)
		}
		return statusMsg(status)
	}
}

func (m *Model) startLogStream() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancelLogs = cancel

		// Find pods
		pods, err := debug.FindPods(ctx, m.client.Clientset, m.namespace, m.appName)
		if err != nil {
			return errMsg(err)
		}

		if len(pods) == 0 {
			return errMsg(fmt.Errorf("no pods found"))
		}

		// Create a channel for log lines
		logChan := make(chan debug.LogLine, 100)
		m.logChan = logChan

		// Start streaming in background
		go func() {
			// Create a custom writer that sends to channel
			writer := &channelWriter{ch: logChan, ctx: ctx}
			opts := debug.DefaultLogsOptions()
			opts.TailLines = 50
			_ = debug.StreamLogs(ctx, m.client.Clientset, m.namespace, pods, opts, writer)
		}()

		return nil
	}
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) restartDeployment() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Get deployment
		dep, err := m.client.Clientset.AppsV1().Deployments(m.namespace).Get(ctx, m.appName, metav1.GetOptions{})
		if err != nil {
			return errMsg(fmt.Errorf("failed to get deployment: %w", err))
		}

		// Add restart annotation
		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = make(map[string]string)
		}
		dep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

		_, err = m.client.Clientset.AppsV1().Deployments(m.namespace).Update(ctx, dep, metav1.UpdateOptions{})
		if err != nil {
			return errMsg(fmt.Errorf("failed to restart: %w", err))
		}

		return nil
	}
}

// channelWriter adapts io.Writer to send log lines to a channel
type channelWriter struct {
	ch  chan<- debug.LogLine
	ctx context.Context
}

func (w *channelWriter) Write(p []byte) (n int, err error) {
	select {
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	default:
		// Parse the log line - this is a simplified version
		// The actual format is: "[source] timestamp message"
		line := string(p)
		w.ch <- debug.LogLine{
			Timestamp: time.Now(),
			Source:    "log",
			Message:   strings.TrimSpace(line),
		}
		return len(p), nil
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
