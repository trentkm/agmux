package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/trent/tmux-workspace/internal/notify"
	"github.com/trent/tmux-workspace/internal/tmux"
)

// ── Palette ──────────────────────────────────────────────────────────
var (
	colorAccent  = lipgloss.Color("#5f87d7") // soft blue
	colorText    = lipgloss.Color("#c0c0c0") // light gray
	colorMuted   = lipgloss.Color("#585858") // dim gray
	colorBright  = lipgloss.Color("#e4e4e4") // near white
	colorWaiting = lipgloss.Color("#d7875f") // warm amber — attention
	colorWorking = lipgloss.Color("#5f87af") // steel blue — in progress
	colorDone    = lipgloss.Color("#5faf5f") // soft green — complete
	colorSep     = lipgloss.Color("#3a3a3a") // subtle separator
)

// ── Styles ───────────────────────────────────────────────────────────
var (
	// Session name styles
	sessionStyle = lipgloss.NewStyle().
			Foreground(colorBright).
			Bold(true)

	sessionDimStyle = lipgloss.NewStyle().
			Foreground(colorText)

	currentMarkerStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	// Cursor marker
	cursorBarStyle = lipgloss.NewStyle().
			Foreground(colorBright).
			Bold(true)

	// Detail text (paths, tree)
	pathStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// Status badges
	waitingStyle = lipgloss.NewStyle().
			Foreground(colorWaiting).
			Bold(true)

	workingStyle = lipgloss.NewStyle().
			Foreground(colorWorking)

	doneStyle = lipgloss.NewStyle().
			Foreground(colorDone)

	// Summary header
	summaryStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// Footer
	footerKeyStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	footerDescStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// Separators
	sepStyle = lipgloss.NewStyle().
			Foreground(colorSep)

	emptyStyle = lipgloss.NewStyle().
			Foreground(colorMuted)
)

// ── Model ────────────────────────────────────────────────────────────

type sessionEntry struct {
	session     tmux.Session
	windows     []tmux.Window
	notif       *notify.Notification
	simple      bool
	path        string
	agentName   string
	agentStatus tmux.AgentStatus
}

type Model struct {
	entries        []sessionEntry
	cursor         int
	currentSession string
	viewport       viewport.Model
	width          int
	height         int
	ready          bool
	cmdMode        bool
	cmdBuf         string
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func NewModel() Model {
	m := Model{
		currentSession: tmux.ClientSession(),
	}
	m.loadSessions()
	return m
}

func (m *Model) loadSessions() {
	sessions, err := tmux.ListSessions()
	if err != nil {
		return
	}

	m.currentSession = tmux.ClientSession()

	entries := make([]sessionEntry, 0, len(sessions))
	for _, s := range sessions {
		wins, _ := tmux.ListWindowsWithPanes(s.Name)
		agentName, agentStatus := tmux.SessionAgentStatus(wins)
		entry := sessionEntry{
			session:     s,
			windows:     wins,
			notif:       notify.Get(s.Name),
			agentName:   agentName,
			agentStatus: agentStatus,
		}
		classifyEntry(&entry)
		entries = append(entries, entry)
	}
	m.entries = entries

	if m.cursor >= len(m.entries) {
		m.cursor = max(0, len(m.entries)-1)
	}
}

func classifyEntry(e *sessionEntry) {
	if len(e.windows) != 1 {
		e.simple = false
		return
	}
	win := e.windows[0]
	nonShell := 0
	for _, p := range win.Panes {
		if !tmux.IsShell(p.Command) {
			nonShell++
		}
	}
	if nonShell <= 1 {
		e.simple = true
		e.path = tildefy(windowPath(win))
		return
	}
	e.simple = false
}

func (m Model) Init() tea.Cmd {
	return tickCmd()
}

// ── Update ───────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		contentHeight := m.height - chromeHeight
		if contentHeight < 1 {
			contentHeight = 1
		}
		if !m.ready {
			m.viewport = viewport.New(m.width, contentHeight)
			m.ready = true
		} else {
			m.viewport.Width = m.width
			m.viewport.Height = contentHeight
		}
		m.viewport.SetContent(m.renderSessions())
		return m, nil

	case tickMsg:
		m.loadSessions()
		if m.ready {
			m.viewport.SetContent(m.renderSessions())
		}
		return m, tickCmd()

	case tea.KeyMsg:
		if m.cmdMode {
			switch msg.String() {
			case "enter":
				cmd := m.cmdBuf
				m.cmdMode = false
				m.cmdBuf = ""
				switch cmd {
				case "q", "qa", "q!", "qa!":
					return m, tea.Quit
				}
			case "esc":
				m.cmdMode = false
				m.cmdBuf = ""
			case "backspace":
				if len(m.cmdBuf) > 0 {
					m.cmdBuf = m.cmdBuf[:len(m.cmdBuf)-1]
				} else {
					m.cmdMode = false
				}
			default:
				m.cmdBuf += msg.String()
			}
			return m, nil
		}

		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "esc"))):
			return m, tea.Quit

		case key.Matches(msg, key.NewBinding(key.WithKeys(":"))):
			m.cmdMode = true
			m.cmdBuf = ""

		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			if m.cursor < len(m.entries)-1 {
				m.cursor++
				m.viewport.SetContent(m.renderSessions())
				m.ensureCursorVisible()
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			if m.cursor > 0 {
				m.cursor--
				m.viewport.SetContent(m.renderSessions())
				m.ensureCursorVisible()
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("G"))):
			m.cursor = len(m.entries) - 1
			m.viewport.SetContent(m.renderSessions())
			m.viewport.GotoBottom()

		case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
			m.cursor = 0
			m.viewport.SetContent(m.renderSessions())
			m.viewport.GotoTop()

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			if len(m.entries) > 0 {
				selected := m.entries[m.cursor].session.Name
				tmux.SwitchClient(selected)
				return m, tea.Quit
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("c"))):
			if len(m.entries) > 0 {
				notify.Clear(m.entries[m.cursor].session.Name)
				m.loadSessions()
				m.viewport.SetContent(m.renderSessions())
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("C"))):
			notify.ClearAll()
			m.loadSessions()
			m.viewport.SetContent(m.renderSessions())
		}
	}
	return m, nil
}

// ── View ─────────────────────────────────────────────────────────────

const chromeHeight = 2 // footer + gap

func (m Model) View() string {
	if !m.ready {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	b.WriteString(m.renderFooter())
	return b.String()
}

func (m Model) renderFooter() string {
	if m.cmdMode {
		return " " + pathStyle.Render(":"+m.cmdBuf) + "█"
	}
	keys := []struct{ key, desc string }{
		{"j/k", "navigate"},
		{"↵", "switch"},
		{"c", "clear"},
		{"esc", "close"},
	}
	var parts []string
	for _, k := range keys {
		parts = append(parts,
			footerKeyStyle.Render(k.key)+footerDescStyle.Render(" "+k.desc))
	}
	return " " + strings.Join(parts, footerDescStyle.Render("  "))
}

// ── Session list (viewport content) ─────────────────────────────────

func (m Model) renderSessions() string {
	if len(m.entries) == 0 {
		return emptyStyle.Render("\n  No agents running.\n")
	}

	var b strings.Builder
	w := m.contentWidth()

	// ── Summary bar ──
	summary := m.agentSummary()
	if summary != "" {
		b.WriteString(" " + summary)
		b.WriteString("\n")
		b.WriteString(" " + sepStyle.Render(strings.Repeat("─", w)))
		b.WriteString("\n")
	}

	// ── Session entries ──
	for idx, entry := range m.entries {
		isCursor := idx == m.cursor
		isCurrent := entry.session.Name == m.currentSession

		b.WriteString(m.renderEntry(entry, isCursor, isCurrent, w))

		if idx < len(m.entries)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m Model) renderEntry(entry sessionEntry, isCursor, isCurrent bool, w int) string {
	var b strings.Builder

	name := entry.session.Name
	badge := statusBadge(entry)

	// ── Line 1: [marker] name [badge] ──
	var marker string
	if isCursor {
		marker = cursorBarStyle.Render(" ❯ ")
	} else if isCurrent {
		marker = currentMarkerStyle.Render(" ◆ ")
	} else {
		marker = "   "
	}

	nameRendered := sessionDimStyle.Render(name)
	if isCursor || isCurrent {
		nameRendered = sessionStyle.Render(name)
	}

	line1 := marker + nameRendered
	if badge != "" {
		line1 += "  " + badge
	}
	b.WriteString(line1)
	b.WriteString("\n")

	// ── Line 2: path / tree info ──
	if entry.simple {
		detail := entry.path
		if entry.agentStatus == tmux.AgentNone && len(entry.windows) == 1 {
			for _, p := range entry.windows[0].Panes {
				if !tmux.IsShell(p.Command) {
					detail = entry.path + " · " + friendlyName(p)
					break
				}
			}
		}
		b.WriteString(pathStyle.Render("     " + detail))
		b.WriteString("\n")
	} else {
		// Expanded tree for complex sessions
		for _, win := range entry.windows {
			winPath := windowPath(win)
			b.WriteString(pathStyle.Render("     " + winPath))
			b.WriteString("\n")

			nonShellPanes := filterNonShell(win.Panes)
			for _, pane := range nonShellPanes {
				b.WriteString(pathStyle.Render("       └ " + friendlyName(pane)))
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

// ── Status badges ───────────────────────────────────────────────────

func statusBadge(entry sessionEntry) string {
	// Working overrides stale "done" — the agent started a new task
	if entry.agentStatus == tmux.AgentWorking {
		// But "waiting" still wins — agent needs your input
		if entry.notif != nil && entry.notif.Status == notify.StatusWaiting {
			return waitingStyle.Render("● waiting ") + pathStyle.Render(entry.notif.TimeAgo())
		}
		return workingStyle.Render("⟳ working")
	}
	if entry.notif != nil {
		ago := entry.notif.TimeAgo()
		switch entry.notif.Status {
		case notify.StatusWaiting:
			return waitingStyle.Render("● waiting ") + pathStyle.Render(ago)
		case notify.StatusDone:
			return doneStyle.Render("✓ done ") + pathStyle.Render(ago)
		}
	}
	return ""
}

func (m Model) agentSummary() string {
	var working, waiting, done int
	for _, e := range m.entries {
		isWorking := e.agentStatus == tmux.AgentWorking
		hasNotif := e.notif != nil

		switch {
		case hasNotif && e.notif.Status == notify.StatusWaiting:
			waiting++ // waiting always counts
		case isWorking:
			working++ // working overrides done
		case hasNotif && e.notif.Status == notify.StatusDone:
			done++
		}
	}

	var parts []string
	if waiting > 0 {
		parts = append(parts, waitingStyle.Render(fmt.Sprintf("● %d waiting", waiting)))
	}
	if working > 0 {
		parts = append(parts, workingStyle.Render(fmt.Sprintf("⟳ %d working", working)))
	}
	if done > 0 {
		parts = append(parts, doneStyle.Render(fmt.Sprintf("✓ %d done", done)))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, summaryStyle.Render("  "))
}

// ── Tree rendering ──────────────────────────────────────────────────

func filterNonShell(panes []tmux.Pane) []tmux.Pane {
	var result []tmux.Pane
	for _, p := range panes {
		if !tmux.IsShell(p.Command) {
			result = append(result, p)
		}
	}
	return result
}

func windowPath(w tmux.Window) string {
	for _, p := range w.Panes {
		if p.Active {
			return tildefy(p.Path)
		}
	}
	if len(w.Panes) > 0 {
		return tildefy(w.Panes[0].Path)
	}
	return ""
}

func friendlyName(p tmux.Pane) string {
	if strings.Contains(p.Title, "Claude Code") {
		return "claude"
	}
	return p.Command
}

// ── Helpers ──────────────────────────────────────────────────────────

func (m Model) contentWidth() int {
	w := m.width - 2
	if w < 10 {
		w = 20
	}
	return w
}

func (m *Model) ensureCursorVisible() {
	line := 0
	for i := 0; i < m.cursor && i < len(m.entries); i++ {
		line += m.entryHeight(m.entries[i])
		line++ // blank separator
	}

	vpTop := m.viewport.YOffset
	vpBottom := vpTop + m.viewport.Height

	if line < vpTop {
		m.viewport.SetYOffset(line)
	} else if line >= vpBottom {
		m.viewport.SetYOffset(line - m.viewport.Height + m.entryHeight(m.entries[m.cursor]))
	}
}

func (m Model) entryHeight(e sessionEntry) int {
	if e.simple {
		return 2 // name + path
	}
	h := 1 // name line
	for _, w := range e.windows {
		h++ // window path
		h += len(filterNonShell(w.Panes))
	}
	return h
}

func tildefy(path string) string {
	home, _ := os.UserHomeDir()
	if realHome, err := filepath.EvalSymlinks(home); err == nil && realHome != home {
		path = strings.Replace(path, realHome, "~", 1)
	}
	path = strings.Replace(path, home, "~", 1)

	entries, _ := os.ReadDir(home)
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 {
			link := filepath.Join(home, e.Name())
			if target, err := filepath.EvalSymlinks(link); err == nil {
				path = strings.Replace(path, target, "~/"+e.Name(), 1)
			}
		}
	}
	return path
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
