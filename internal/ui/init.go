package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Init wizard styles ──────────────────────────────────────────────

var (
	initTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e4e4e4")).
			Bold(true)

	initDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#585858"))

	initCheckStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5faf5f")).
			Bold(true)

	initUncheckStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#585858"))

	initCursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e4e4e4")).
			Bold(true)

	initHeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5f87d7")).
			Bold(true)

	initCodeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c0c0c0"))

	initSuccessStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#5faf5f")).
				Bold(true)
)

type agent struct {
	name     string
	desc     string
	selected bool
}

type InitModel struct {
	agents   []agent
	cursor   int
	phase    string // "select" or "output"
	output   string
	copied   bool
	width    int
	height   int
}

func NewInitModel() InitModel {
	return InitModel{
		agents: []agent{
			{name: "Claude Code", desc: "~/.claude/settings.json", selected: false},
			{name: "Kiro CLI", desc: "~/.kiro/agents/default.json", selected: false},
		},
		phase: "select",
	}
}

func (m InitModel) Init() tea.Cmd {
	return nil
}

func (m InitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		if m.phase == "output" {
			switch msg.String() {
			case "c":
				if err := copyToClipboard(m.output); err == nil {
					m.copied = true
				}
			case "q", "esc", "enter":
				return m, tea.Quit
			}
			return m, nil
		}

		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "esc"))):
			return m, tea.Quit

		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			if m.cursor < len(m.agents)-1 {
				m.cursor++
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys(" ", "x"))):
			m.agents[m.cursor].selected = !m.agents[m.cursor].selected

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			// If nothing selected, select the current item
			anySelected := false
			for _, a := range m.agents {
				if a.selected {
					anySelected = true
					break
				}
			}
			if !anySelected {
				m.agents[m.cursor].selected = true
			}
			m.output = m.generateConfig()
			m.phase = "output"
		}
	}
	return m, nil
}

func (m InitModel) View() string {
	if m.phase == "output" {
		return m.viewOutput()
	}
	return m.viewSelect()
}

func (m InitModel) viewSelect() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(initTitleStyle.Render("  agmux init"))
	b.WriteString("\n")
	b.WriteString(initDescStyle.Render("  Select your agents:"))
	b.WriteString("\n\n")

	for i, a := range m.agents {
		cursor := "  "
		if i == m.cursor {
			cursor = initCursorStyle.Render("❯ ")
		}

		check := initUncheckStyle.Render("[ ]")
		if a.selected {
			check = initCheckStyle.Render("[✓]")
		}

		name := initDescStyle.Render(a.name)
		if i == m.cursor {
			name = initTitleStyle.Render(a.name)
		}

		b.WriteString(fmt.Sprintf("  %s%s %s  %s\n", cursor, check, name, initDescStyle.Render(a.desc)))
	}

	b.WriteString("\n")
	b.WriteString(initDescStyle.Render("  space select  enter confirm  q quit"))
	b.WriteString("\n")

	return b.String()
}

func (m InitModel) viewOutput() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(m.output)

	if m.copied {
		b.WriteString("\n  " + initSuccessStyle.Render("✓ Copied to clipboard"))
	} else {
		b.WriteString("\n  " + initDescStyle.Render("c copy to clipboard  q quit"))
	}
	b.WriteString("\n")

	return b.String()
}

func (m InitModel) generateConfig() string {
	var b strings.Builder

	// Always show tmux config
	b.WriteString(initHeaderStyle.Render("  ── tmux config ──────────────────────"))
	b.WriteString("\n")
	b.WriteString(initDescStyle.Render("  Add to ~/.tmux.conf:"))
	b.WriteString("\n\n")
	b.WriteString(initCodeStyle.Render("    # Agent popup"))
	b.WriteString("\n")
	b.WriteString(initCodeStyle.Render("    bind b run-shell 'agmux toggle'"))
	b.WriteString("\n")
	b.WriteString(initCodeStyle.Render("    set -g status-right '#(agmux status) %H:%M'"))
	b.WriteString("\n")
	b.WriteString(initCodeStyle.Render("    set -g status-interval 2"))
	b.WriteString("\n")

	for _, a := range m.agents {
		if !a.selected {
			continue
		}

		b.WriteString("\n")

		switch a.name {
		case "Claude Code":
			b.WriteString(initHeaderStyle.Render("  ── Claude Code hooks ────────────────"))
			b.WriteString("\n")
			b.WriteString(initDescStyle.Render("  Add to ~/.claude/settings.json:"))
			b.WriteString("\n\n")
			b.WriteString(initCodeStyle.Render(`    {
      "hooks": {
        "UserPromptSubmit": [
          {
            "hooks": [{
              "type": "command",
              "command": "agmux notify --status working",
              "timeout": 5
            }]
          }
        ],
        "Notification": [
          {
            "matcher": "permission_prompt",
            "hooks": [{
              "type": "command",
              "command": "agmux notify --status waiting",
              "timeout": 5
            }]
          }
        ],
        "Stop": [
          {
            "hooks": [{
              "type": "command",
              "command": "agmux notify --status done",
              "timeout": 5
            }]
          }
        ]
      }
    }`))
			b.WriteString("\n")

		case "Kiro CLI":
			b.WriteString(initHeaderStyle.Render("  ── Kiro CLI hooks ───────────────────"))
			b.WriteString("\n")
			b.WriteString(initDescStyle.Render("  Add to ~/.kiro/agents/default.json:"))
			b.WriteString("\n\n")
			b.WriteString(initCodeStyle.Render(`    {
      "name": "default",
      "description": "Default agent with agmux notification hooks",
      "hooks": {
        "agentSpawn": [
          {
            "command": "agmux notify --status done 2>/dev/null",
            "description": "Register with agmux as soon as Kiro starts",
            "timeout_ms": 5000
          }
        ],
        "userPromptSubmit": [
          {
            "command": "agmux notify --status working 2>/dev/null",
            "description": "Notify agmux that Kiro is working",
            "timeout_ms": 5000
          }
        ],
        "stop": [
          {
            "command": "agmux notify --status done 2>/dev/null",
            "description": "Notify agmux that Kiro finished its turn",
            "timeout_ms": 5000
          }
        ]
      }
    }`))
			b.WriteString("\n")
			b.WriteString("\n")
			b.WriteString(initDescStyle.Render("  Then start with: kiro-cli chat --agent default"))
			b.WriteString("\n")
		}
	}

	// macOS notification tip
	b.WriteString("\n")
	b.WriteString(initHeaderStyle.Render("  ── macOS notifications (optional) ──"))
	b.WriteString("\n")
	b.WriteString(initDescStyle.Render("  For click-to-switch notifications:"))
	b.WriteString("\n\n")
	b.WriteString(initCodeStyle.Render("    brew install terminal-notifier"))
	b.WriteString("\n")

	return b.String()
}

func copyToClipboard(text string) error {
	// Strip ANSI escape codes for clipboard
	clean := stripAnsi(text)
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("pbcopy")
	} else {
		cmd = exec.Command("xclip", "-selection", "clipboard")
	}
	cmd.Stdin = strings.NewReader(clean)
	return cmd.Run()
}

func stripAnsi(s string) string {
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}
