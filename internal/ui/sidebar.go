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
	colorAccent  = lipgloss.Color("4")  // blue  — current session marker
	colorHeader  = lipgloss.Color("6")  // cyan  — header chrome
	colorText    = lipgloss.Color("7")  // light — primary text
	colorMuted   = lipgloss.Color("8")  // gray  — secondary info
	colorBright  = lipgloss.Color("15") // white — emphasis
	colorNotif   = lipgloss.Color("3")  // yellow — notifications
	colorCursorB = lipgloss.Color("8")  // cursor bar background
)

// ── Styles ───────────────────────────────────────────────────────────
var (
	headerStyle = lipgloss.NewStyle().
			Foreground(colorHeader).
			Bold(true)

	dividerStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	cursorStyle = lipgloss.NewStyle().
			Background(colorCursorB).
			Foreground(colorBright).
			Bold(true)

	cursorCurrentStyle = lipgloss.NewStyle().
				Background(colorAccent).
				Foreground(colorBright).
				Bold(true)

	currentStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	normalStyle = lipgloss.NewStyle().
			Foreground(colorText)

	detailStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	notifDotStyle = lipgloss.NewStyle().
			Foreground(colorNotif).
			Bold(true)

	notifMsgStyle = lipgloss.NewStyle().
			Foreground(colorNotif)

	notifTimeStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	footerKeyStyle = lipgloss.NewStyle().
			Foreground(colorHeader).
			Bold(true)

	footerDescStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	emptyStyle = lipgloss.NewStyle().
			Foreground(colorMuted)
)

const sidebarPaneTitle = "tw-sidebar"

// ── Model ────────────────────────────────────────────────────────────

type sessionEntry struct {
	session tmux.Session
	windows []tmux.Window // filtered: no sidebar panes
	notif   *notify.Notification
	simple  bool   // true if collapsible to one line
	path    string // primary path for simple sessions
}

type Model struct {
	entries        []sessionEntry
	cursor         int
	currentSession string
	viewport       viewport.Model
	width          int
	height         int
	ready          bool
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
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
		filtered := filterWindows(wins)
		entry := sessionEntry{
			session: s,
			windows: filtered,
			notif:   notify.Get(s.Name),
		}
		classifyEntry(&entry)
		entries = append(entries, entry)
	}
	m.entries = entries

	if m.cursor >= len(m.entries) {
		m.cursor = max(0, len(m.entries)-1)
	}
}

// filterWindows removes sidebar panes and windows that become empty after filtering.
func filterWindows(windows []tmux.Window) []tmux.Window {
	var result []tmux.Window
	for _, w := range windows {
		var panes []tmux.Pane
		for _, p := range w.Panes {
			if p.Title == sidebarPaneTitle {
				continue
			}
			panes = append(panes, p)
		}
		if len(panes) > 0 {
			filtered := w
			filtered.Panes = panes
			result = append(result, filtered)
		}
	}
	return result
}

// classifyEntry determines if a session can be collapsed to one line.
// Simple = one window with no interesting sub-structure to show.
func classifyEntry(e *sessionEntry) {
	if len(e.windows) != 1 {
		e.simple = false
		return
	}
	win := e.windows[0]
	// Count non-shell panes
	nonShell := 0
	for _, p := range win.Panes {
		if !tmux.IsShell(p.Command) {
			nonShell++
		}
	}
	// Simple if: only shells, or only one non-shell process (nothing to tree)
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
		contentHeight := m.height - m.chromeHeight()
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
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "esc"))):
			return m, tea.Quit

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
				notify.Clear(selected)
				tmux.SwitchClient(selected)
				m.loadSessions()
				m.viewport.SetContent(m.renderSessions())
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

func (m Model) View() string {
	if !m.ready {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	b.WriteString(m.renderFooter())
	return b.String()
}

func (m Model) chromeHeight() int {
	return 5 // header(2) + footer(2) + 1 newline
}

func (m Model) renderHeader() string {
	w := m.contentWidth()
	title := headerStyle.Render(" WORKSPACES")

	notifCount := 0
	for _, e := range m.entries {
		if e.notif != nil {
			notifCount++
		}
	}
	badge := ""
	if notifCount > 0 {
		badge = notifDotStyle.Render(fmt.Sprintf(" %d●", notifCount))
	}

	divider := dividerStyle.Render(" " + strings.Repeat("─", w))
	return title + badge + "\n" + divider + "\n"
}

func (m Model) renderFooter() string {
	w := m.contentWidth()
	divider := dividerStyle.Render(" " + strings.Repeat("─", w))

	keys := []struct{ key, desc string }{
		{"j/k", "navigate"},
		{"enter", "switch"},
		{"c", "clear"},
		{"q", "close"},
	}
	var parts []string
	for _, k := range keys {
		parts = append(parts,
			footerKeyStyle.Render(k.key)+" "+footerDescStyle.Render(k.desc))
	}
	help := " " + strings.Join(parts, footerDescStyle.Render("  "))

	return divider + "\n" + help
}

// ── Session list (viewport content) ─────────────────────────────────

func (m Model) renderSessions() string {
	if len(m.entries) == 0 {
		return emptyStyle.Render("\n  No sessions running.\n")
	}

	var b strings.Builder
	w := m.contentWidth()

	for idx, entry := range m.entries {
		isCursor := idx == m.cursor
		isCurrent := entry.session.Name == m.currentSession
		isLast := idx == len(m.entries)-1

		if entry.simple {
			// ── Collapsed: session + path on one line ──
			b.WriteString(m.renderSimpleSession(entry, isCursor, isCurrent, w))
			b.WriteString("\n")
		} else {
			// ── Expanded: session header + window/pane tree ──
			b.WriteString(m.renderSessionLine(entry.session.Name, isCursor, isCurrent, entry.notif, "", w))
			b.WriteString("\n")
			b.WriteString(m.renderTree(entry, isCursor))
		}

		// Notification message
		if entry.notif != nil {
			msg := truncate(entry.notif.Message, w-8)
			b.WriteString(notifMsgStyle.Render(fmt.Sprintf("    ↳ %s", msg)))
			b.WriteString("\n")
		}

		if !isLast {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// renderSimpleSession renders a collapsed session: "▸ session-name  ~/path"
// If there's a non-shell process running, adds it: "▸ session-name  ~/path  claude"
func (m Model) renderSimpleSession(entry sessionEntry, isCursor, isCurrent bool, w int) string {
	suffix := entry.path
	if len(entry.windows) == 1 {
		for _, p := range entry.windows[0].Panes {
			if !tmux.IsShell(p.Command) {
				suffix = entry.path + "  " + friendlyName(p)
				break
			}
		}
	}
	return m.renderSessionLine(entry.session.Name, isCursor, isCurrent, entry.notif, suffix, w)
}

func (m Model) renderSessionLine(name string, isCursor, isCurrent bool, n *notify.Notification, path string, w int) string {
	var prefix string
	switch {
	case isCursor && isCurrent:
		prefix = "◆ "
	case isCursor:
		prefix = "▸ "
	case isCurrent:
		prefix = "◆ "
	default:
		prefix = "  "
	}
	label := prefix + name

	// Path suffix for simple sessions
	pathSuffix := ""
	if path != "" {
		pathSuffix = "  " + path
	}

	// Notification badge
	badge := ""
	if n != nil {
		badge = " " + notifDotStyle.Render("●") + " " + notifTimeStyle.Render(n.TimeAgo())
	}

	if isCursor {
		style := cursorStyle
		if isCurrent {
			style = cursorCurrentStyle
		}
		// Build: label + path (dimmed) + padding + badge
		plainText := label + pathSuffix
		textWidth := lipgloss.Width(plainText) + lipgloss.Width(badge)
		padding := w + 2 - textWidth
		if padding < 0 {
			padding = 0
		}
		padded := label + pathSuffix + strings.Repeat(" ", padding) + badge
		return style.Render(padded)
	}

	// Non-cursor: style label and path separately
	var line string
	if isCurrent {
		line = currentStyle.Render(label)
	} else {
		line = normalStyle.Render(label)
	}
	if pathSuffix != "" {
		line += detailStyle.Render(pathSuffix)
	}
	line += badge
	return line
}

// renderTree renders the window → pane hierarchy for expanded sessions.
func (m Model) renderTree(entry sessionEntry, isCursor bool) string {
	var b strings.Builder

	for wi, win := range entry.windows {
		isLastWin := wi == len(entry.windows)-1 && entry.notif == nil

		// Window connector
		winConn := "├─"
		if isLastWin {
			winConn = "└─"
		}
		childPrefix := "│  "
		if isLastWin {
			childPrefix = "   "
		}

		// Window line: show the path of the active pane (or first pane)
		winPath := windowPath(win)

		b.WriteString(detailStyle.Render("    " + winConn + " "))
		b.WriteString(detailStyle.Render(winPath))
		b.WriteString("\n")

		// Panes: only show if >1 pane in this window
		if len(win.Panes) > 1 {
			for pi, pane := range win.Panes {
				if tmux.IsShell(pane.Command) {
					continue // skip shell panes in the tree
				}
				isLastPane := pi == len(win.Panes)-1
				paneConn := "├─"
				if isLastPane {
					paneConn = "└─"
				}

				label := friendlyName(pane)
				b.WriteString(detailStyle.Render(fmt.Sprintf("    %s %s %s", childPrefix, paneConn, label)))
				b.WriteString("\n")
			}
		} else if len(win.Panes) == 1 && !tmux.IsShell(win.Panes[0].Command) {
			// Single non-shell pane: show what's running
			label := friendlyName(win.Panes[0])
			b.WriteString(detailStyle.Render(fmt.Sprintf("    %s └─ %s", childPrefix, label)))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// ── Helpers ──────────────────────────────────────────────────────────

// windowPath returns the display path for a window, using the active pane's path.
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

// friendlyName returns a human-readable name for a pane process.
func friendlyName(p tmux.Pane) string {
	// Check pane title for recognizable names
	title := p.Title
	if strings.Contains(title, "Claude Code") {
		return "claude"
	}
	// Fall back to command name
	return p.Command
}

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
		h := 1
		if e.notif != nil {
			h++
		}
		return h
	}
	// Session line + windows + pane sub-trees
	h := 1
	for _, w := range e.windows {
		h++ // window line
		if len(w.Panes) > 1 {
			for _, p := range w.Panes {
				if !tmux.IsShell(p.Command) {
					h++
				}
			}
		} else if len(w.Panes) == 1 && !tmux.IsShell(w.Panes[0].Command) {
			h++
		}
	}
	if e.notif != nil {
		h++
	}
	return h
}

func tildefy(path string) string {
	home, _ := os.UserHomeDir()
	// Resolve symlinks in home to catch cases like ~/repos -> /Volumes/repos
	if realHome, err := filepath.EvalSymlinks(home); err == nil && realHome != home {
		path = strings.Replace(path, realHome, "~", 1)
	}
	path = strings.Replace(path, home, "~", 1)

	// Also resolve common symlinked dirs under home
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
