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
	"github.com/trentkm/agmux/internal/notify"
	"github.com/trentkm/agmux/internal/tmux"
)

// ── Palette ──────────────────────────────────────────────────────────
var (
	colorAccent  = lipgloss.Color("#5f87d7") // soft blue
	colorText    = lipgloss.Color("#c0c0c0") // light gray
	colorMuted   = lipgloss.Color("#585858") // dim gray
	colorBright  = lipgloss.Color("#e4e4e4") // near white
	colorWaiting = lipgloss.Color("#e5a84b") // warm gold — attention
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
	// paneAgents caches DetectAgent results keyed by pane index
	paneAgents map[int]paneAgent
}

type paneAgent struct {
	name   string
	status tmux.AgentStatus
}

func (e *sessionEntry) detectPane(p tmux.Pane) (string, tmux.AgentStatus) {
	if e.paneAgents == nil {
		e.paneAgents = make(map[int]paneAgent)
	}
	if cached, ok := e.paneAgents[p.Pid]; ok {
		return cached.name, cached.status
	}
	name, status := tmux.DetectAgent(p)
	e.paneAgents[p.Pid] = paneAgent{name, status}
	return name, status
}

type Model struct {
	entries        []sessionEntry
	filtered       []int // indices into entries matching search
	cursor         int   // cursor into filtered list
	currentSession string
	viewport       viewport.Model
	width          int
	height         int
	ready          bool
	cmdMode        bool
	cmdBuf         string
	searchMode     bool
	searchBuf      string
	newMode        bool   // typing a new session name
	newBuf         string
	animFrame      int // animation frame counter
}

type tickMsg time.Time
type animMsg time.Time

// sessionsMsg carries async-loaded session data back to the event loop.
type sessionsMsg struct {
	entries        []sessionEntry
	currentSession string
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func animCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return animMsg(t)
	})
}

func NewModel() Model {
	m := Model{
		currentSession: tmux.ClientSession(),
	}
	m.loadSessions()
	return m
}

// loadSessionsCmd returns a tea.Cmd that loads session data in the background.
func loadSessionsCmd() tea.Msg {
	sessions, err := tmux.ListSessions()
	if err != nil {
		return sessionsMsg{}
	}
	current := tmux.ClientSession()
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
	return sessionsMsg{entries: entries, currentSession: current}
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
	m.applyFilter()
}

func (m *Model) applyFilter() {
	if m.searchBuf == "" {
		// No filter — show all
		m.filtered = make([]int, len(m.entries))
		for i := range m.entries {
			m.filtered[i] = i
		}
	} else {
		query := strings.ToLower(m.searchBuf)
		m.filtered = nil
		for i, e := range m.entries {
			name := strings.ToLower(e.session.Name)
			path := strings.ToLower(e.path)
			if strings.Contains(name, query) || strings.Contains(path, query) {
				m.filtered = append(m.filtered, i)
			}
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

func classifyEntry(e *sessionEntry) {
	if len(e.windows) > 0 {
		e.path = tildefy(windowPath(e.windows[0]))
	}

	agentCount := 0
	for _, w := range e.windows {
		for _, p := range w.Panes {
			if name, _ := e.detectPane(p); name != "" {
				agentCount++
			}
		}
	}

	e.simple = agentCount <= 1
}

// Animation frames — block density pulse
var pulseFrames = []string{"░", "▒", "▓", "█", "▓", "▒", "░", " "}

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadSessionsCmd, animCmd())
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

	case sessionsMsg:
		m.entries = msg.entries
		m.currentSession = msg.currentSession
		m.applyFilter()
		if m.ready {
			m.viewport.SetContent(m.renderSessions())
		}
		// Schedule next refresh
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})

	case tickMsg:
		// Kick off async load — doesn't block the event loop
		return m, loadSessionsCmd

	case animMsg:
		m.animFrame = (m.animFrame + 1) % len(pulseFrames)
		if m.ready && m.hasWorkingAgent() {
			m.viewport.SetContent(m.renderSessions())
		}
		return m, animCmd()

	case tea.KeyMsg:
		// ── Command mode (:q) ──
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

		// ── Search mode (/) ──
		if m.searchMode {
			switch msg.String() {
			case "enter":
				// Confirm search — stay filtered, exit search mode
				m.searchMode = false
			case "esc":
				// Cancel search — clear filter
				m.searchMode = false
				m.searchBuf = ""
				m.applyFilter()
				m.viewport.SetContent(m.renderSessions())
			case "backspace":
				if len(m.searchBuf) > 0 {
					m.searchBuf = m.searchBuf[:len(m.searchBuf)-1]
					m.applyFilter()
					m.viewport.SetContent(m.renderSessions())
				} else {
					m.searchMode = false
					m.applyFilter()
					m.viewport.SetContent(m.renderSessions())
				}
			default:
				ch := msg.String()
				if len(ch) == 1 {
					m.searchBuf += ch
					m.applyFilter()
					m.viewport.SetContent(m.renderSessions())
				}
			}
			return m, nil
		}

		// ── New session mode (n) ──
		if m.newMode {
			switch msg.String() {
			case "enter":
				name := m.newBuf
				m.newMode = false
				m.newBuf = ""
				if name != "" {
					tmux.Run("new-session", "-d", "-s", name)
					tmux.SwitchClient(name)
					return m, tea.Quit
				}
			case "esc":
				m.newMode = false
				m.newBuf = ""
			case "backspace":
				if len(m.newBuf) > 0 {
					m.newBuf = m.newBuf[:len(m.newBuf)-1]
				} else {
					m.newMode = false
				}
			default:
				ch := msg.String()
				if len(ch) == 1 {
					m.newBuf += ch
				}
			}
			return m, nil
		}

		// ── Normal mode ──
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "esc"))):
			return m, tea.Quit

		case key.Matches(msg, key.NewBinding(key.WithKeys(":"))):
			m.cmdMode = true
			m.cmdBuf = ""

		case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
			m.searchMode = true
			m.searchBuf = ""

		case key.Matches(msg, key.NewBinding(key.WithKeys("n"))):
			m.newMode = true
			m.newBuf = ""

		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
				m.updateView()
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			if m.cursor > 0 {
				m.cursor--
				m.updateView()
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("G"))):
			m.cursor = len(m.filtered) - 1
			m.updateView()
			m.viewport.GotoBottom()

		case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
			m.cursor = 0
			m.updateView()
			m.viewport.GotoTop()

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			if e := m.selectedEntry(); e != nil {
				tmux.SwitchClient(e.session.Name)
				return m, tea.Quit
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("c"))):
			if e := m.selectedEntry(); e != nil {
				notify.Clear(e.session.Name)
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
	if m.searchMode {
		return " " + footerKeyStyle.Render("/") + pathStyle.Render(m.searchBuf) + "█"
	}
	if m.newMode {
		return " " + footerKeyStyle.Render("new: ") + pathStyle.Render(m.newBuf) + "█"
	}
	// Show active filter indicator
	if m.searchBuf != "" {
		filter := footerDescStyle.Render("filter: ") + footerKeyStyle.Render(m.searchBuf) + footerDescStyle.Render("  esc clear")
		return " " + filter
	}
	keys := []struct{ key, desc string }{
		{"j/k", "navigate"},
		{"↵", "switch"},
		{"/", "search"},
		{"n", "new"},
		{"c", "clear"},
		{"q", "close"},
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
	if len(m.filtered) == 0 && m.searchBuf != "" {
		b.WriteString(emptyStyle.Render("\n  No matching sessions.\n"))
	}
	for fi, idx := range m.filtered {
		entry := &m.entries[idx]
		isCursor := fi == m.cursor
		isCurrent := entry.session.Name == m.currentSession

		b.WriteString(m.renderEntry(entry, isCursor, isCurrent, w))

		if fi < len(m.filtered)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m Model) renderEntry(entry *sessionEntry, isCursor, isCurrent bool, w int) string {
	highlightBg := lipgloss.Color("#333333")

	name := entry.session.Name
	badge := m.statusBadge(*entry)

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

	// ── Detail lines ──
	type agentPane struct {
		pane   tmux.Pane
		winIdx int
	}
	var allAgents []agentPane
	windowsWithAgents := 0
	for _, win := range entry.windows {
		hasAgent := false
		for _, p := range win.Panes {
			if name, status := entry.detectPane(p); name != "" && status != tmux.AgentNone {
				allAgents = append(allAgents, agentPane{p, win.Index})
				hasAgent = true
			}
		}
		if hasAgent {
			windowsWithAgents++
		}
	}
	showWindowTag := windowsWithAgents > 1

	var detailLines []string
	if len(allAgents) == 0 {
		detailLines = append(detailLines, pathStyle.Render("     "+entry.path))
	} else if len(allAgents) == 1 {
		p := allAgents[0].pane
		detailLines = append(detailLines, m.renderPaneDetail(p, entry, false, "     ", ""))
	} else {
		for _, ap := range allAgents {
			winTag := ""
			if showWindowTag {
				winTag = fmt.Sprintf("  w%d", ap.winIdx)
			}
			detailLines = append(detailLines, m.renderPaneDetail(ap.pane, entry, true, "     └ ", winTag))
		}
	}

	// ── Compose with optional highlight background ──
	var b strings.Builder
	if isCursor {
		b.WriteString(m.padWithBg(line1, w, highlightBg))
		for _, dl := range detailLines {
			b.WriteString("\n")
			b.WriteString(m.padWithBg(dl, w, highlightBg))
		}
	} else {
		b.WriteString(line1)
		for _, dl := range detailLines {
			b.WriteString("\n")
			b.WriteString(dl)
		}
	}
	b.WriteString("\n")

	return b.String()
}

// renderPaneDetail renders a single agent pane line with status indicator.
func (m Model) renderPaneDetail(p tmux.Pane, entry *sessionEntry, showConnector bool, prefix, winTag string) string {
	panePath := tildefy(p.Path)
	agentName, paneStatus := entry.detectPane(p)
	if agentName == "" {
		agentName = friendlyName(p)
	}

	var indicator string
	frame := pulseFrames[m.animFrame%len(pulseFrames)]

	// Pane-level: use spinner detection OR notify status
	// Only apply notify status to the pane whose path matches the notify cwd
	notifMatchesPane := entry.notif != nil && (entry.notif.Cwd == "" || entry.notif.Cwd == p.Path)
	notifWorking := notifMatchesPane && entry.notif.Status == notify.StatusWorking
	notifWaiting := notifMatchesPane && entry.notif.Status == notify.StatusWaiting
	notifDone := notifMatchesPane && entry.notif.Status == notify.StatusDone

	switch {
	case paneStatus == tmux.AgentWorking || notifWorking:
		indicator = workingStyle.Render(frame) + pathStyle.Render(" "+agentName+"  "+panePath)
	case notifWaiting:
		indicator = waitingStyle.Render("●") + pathStyle.Render(" "+agentName+"  "+panePath)
	case notifDone:
		indicator = doneStyle.Render("·") + pathStyle.Render(" "+agentName+"  "+panePath)
	default:
		if agentName != "" {
			indicator = doneStyle.Render("·") + pathStyle.Render(" "+agentName+"  "+panePath)
		} else {
			indicator = pathStyle.Render("· "+agentName+"  "+panePath)
		}
	}

	return pathStyle.Render(prefix) + indicator + pathStyle.Render(winTag)
}

func (m Model) padWithBg(content string, w int, bg lipgloss.Color) string {
	contentWidth := lipgloss.Width(content)
	padding := m.width - contentWidth
	if padding < 0 {
		padding = 0
	}
	// Replace every \x1b[0m (reset) with \x1b[0m\x1b[48;2;51;51;51m (reset then re-apply bg)
	// This keeps the background alive through inner style resets
	bgOn := "\x1b[48;2;51;51;51m"
	patched := strings.ReplaceAll(content, "\x1b[0m", "\x1b[0m"+bgOn)
	return bgOn + patched + strings.Repeat(" ", padding) + "\x1b[0m"
}

// ── Status badges ───────────────────────────────────────────────────

func (m Model) statusBadge(entry sessionEntry) string {
	// Spinner detection overrides everything
	if entry.agentStatus == tmux.AgentWorking {
		return workingStyle.Render("⟳ working")
	}
	if entry.notif != nil {
		ago := entry.notif.TimeAgo()
		switch entry.notif.Status {
		case notify.StatusWorking:
			return workingStyle.Render("⟳ working")
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
		if e.agentStatus == tmux.AgentWorking {
			working++
		} else if e.notif != nil {
			switch e.notif.Status {
			case notify.StatusWorking:
				working++
			case notify.StatusWaiting:
				waiting++
			case notify.StatusDone:
				done++
			}
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
	switch p.Command {
	case "kiro-cli", "kiro":
		return "kiro"
	case "codex":
		return "codex"
	case "aider":
		return "aider"
	case "goose":
		return "goose"
	}
	return p.Command
}

// ── Helpers ──────────────────────────────────────────────────────────

func (m *Model) updateView() {
	m.viewport.SetContent(m.renderSessions())
	m.ensureCursorVisible()
}

func (m Model) contentWidth() int {
	w := m.width - 2
	if w < 10 {
		w = 20
	}
	return w
}

func (m Model) hasWorkingAgent() bool {
	for _, e := range m.entries {
		if e.agentStatus == tmux.AgentWorking {
			return true
		}
		if e.notif != nil && e.notif.Status == notify.StatusWorking {
			return true
		}
	}
	return false
}

// selectedEntry returns the entry under the cursor, or nil if nothing is selected.
func (m Model) selectedEntry() *sessionEntry {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return nil
	}
	return &m.entries[m.filtered[m.cursor]]
}

func (m *Model) ensureCursorVisible() {
	line := 0
	for i := 0; i < m.cursor && i < len(m.filtered); i++ {
		line += m.entryHeight(&m.entries[m.filtered[i]])
		line++ // blank separator
	}

	vpTop := m.viewport.YOffset
	vpBottom := vpTop + m.viewport.Height

	if line < vpTop {
		m.viewport.SetYOffset(line)
	} else if line >= vpBottom {
		m.viewport.SetYOffset(line - m.viewport.Height + m.entryHeight(&m.entries[m.cursor]))
	}
}

func (m Model) entryHeight(e *sessionEntry) int {
	h := 1 // session name line
	totalAgents := 0
	for _, w := range e.windows {
		for _, p := range w.Panes {
			if name, _ := e.detectPane(p); name != "" {
				totalAgents++
			}
		}
	}
	if totalAgents <= 1 {
		h++ // compact single line
	} else {
		h += totalAgents // one line per agent
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
