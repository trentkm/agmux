package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/trentkm/agmux/internal/notify"
	"github.com/trentkm/agmux/internal/tmux"
	"github.com/trentkm/agmux/internal/ui"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "agmux",
		Short: "tmux workspace manager",
		Long:  "A workspace manager for tmux with notifications and a visual sidebar.",
	}

	// agmux popup — run the TUI directly (called by display-popup)
	popupCmd := &cobra.Command{
		Use:    "popup",
		Short:  "Open the workspace switcher TUI",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			m := ui.NewModel()
			p := tea.NewProgram(m, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	}

	// agmux toggle — open or dismiss the popup
	toggleCmd := &cobra.Command{
		Use:   "toggle",
		Short: "Toggle the workspace switcher popup",
		RunE: func(cmd *cobra.Command, args []string) error {
			width, _ := cmd.Flags().GetString("width")
			height, _ := cmd.Flags().GetString("height")
			return togglePopup(width, height)
		},
	}
	toggleCmd.Flags().StringP("width", "w", "45%", "Popup width (columns or percentage)")
	toggleCmd.Flags().StringP("height", "H", "40%", "Popup height (rows or percentage)")

	// agmux notify --status waiting|done
	notifyCmd := &cobra.Command{
		Use:   "notify",
		Short: "Send a notification for the current tmux session",
		RunE: func(cmd *cobra.Command, args []string) error {
			session, _ := cmd.Flags().GetString("session")
			if session == "" && os.Getenv("TMUX") != "" {
				session = tmux.CurrentSession()
			}
			if session == "" {
				session = tmux.SessionForCwd()
			}
			if session == "" {
				session = "default"
			}
			status, _ := cmd.Flags().GetString("status")
			switch status {
			case "working", "waiting", "done":
				return notify.Add(session, notify.Status(status))
			default:
				return fmt.Errorf("--status must be 'working', 'waiting', or 'done'")
			}
		},
	}
	notifyCmd.Flags().StringP("session", "s", "", "Session name (default: current tmux session)")
	notifyCmd.Flags().String("status", "", "Notification status: waiting or done")

	// agmux clear
	clearCmd := &cobra.Command{
		Use:   "clear [session]",
		Short: "Clear notifications",
		RunE: func(cmd *cobra.Command, args []string) error {
			all, _ := cmd.Flags().GetBool("all")
			if all {
				return notify.ClearAll()
			}
			if len(args) > 0 {
				return notify.Clear(args[0])
			}
			session := tmux.CurrentSession()
			if session != "" {
				return notify.Clear(session)
			}
			return fmt.Errorf("specify a session name or use --all")
		},
	}
	clearCmd.Flags().BoolP("all", "a", false, "Clear all notifications")

	// agmux status
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Status bar widget (for tmux status-right)",
		Run: func(cmd *cobra.Command, args []string) {
			sessions, _ := tmux.ListSessions()
			tmux.ResetProcessTree()

			type agentInfo struct {
				name   string
				symbol string
				color  string
			}
			var agents []agentInfo

			var working, waiting, done int
			// Cycle working symbol based on current second
			workSymbols := []string{"░", "▒", "▓", "█", "▓", "▒", "░", " "}
			workSym := workSymbols[time.Now().Second()%len(workSymbols)]

			for _, s := range sessions {
				wins, _ := tmux.ListWindowsWithPanes(s.Name)
				_, agentStatus := tmux.SessionAgentStatus(wins)
				n := notify.Get(s.Name)

				switch {
				case agentStatus == tmux.AgentWorking:
					working++
					agents = append(agents, agentInfo{s.Name, workSym, "#5f87af"})
				case n != nil && n.Status == notify.StatusWorking:
					working++
					agents = append(agents, agentInfo{s.Name, workSym, "#5f87af"})
				case n != nil && n.Status == notify.StatusWaiting:
					waiting++
					agents = append(agents, agentInfo{s.Name, "●", "#e5a84b"})
				case n != nil && n.Status == notify.StatusDone:
					done++
					agents = append(agents, agentInfo{s.Name, "✓", "#5faf5f"})
				}
			}

			if len(agents) == 0 {
				return
			}

			// Compact mode when >4 active agents
			if len(agents) > 4 {
				var parts []string
				if working > 0 {
					parts = append(parts, fmt.Sprintf("#[fg=#5f87af]%d%s#[default]", working, workSym))
				}
				if waiting > 0 {
					parts = append(parts, fmt.Sprintf("#[fg=#e5a84b,bold]%d●#[default]", waiting))
				}
				if done > 0 {
					parts = append(parts, fmt.Sprintf("#[fg=#5faf5f]%d✓#[default]", done))
				}
				fmt.Print(" " + strings.Join(parts, " ") + " ")
				return
			}

			// Named mode
			var parts []string
			for _, a := range agents {
				parts = append(parts, fmt.Sprintf("#[fg=%s]%s%s#[default]", a.color, a.name, a.symbol))
			}
			fmt.Print(" " + strings.Join(parts, "  ") + " ")
		},
	}

	// agmux switch <session> — switch to a session (works from outside tmux)
	switchCmd := &cobra.Command{
		Use:    "switch [session]",
		Short:  "Switch to a tmux session",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			session := args[0]
			// Find the most recent client and switch it
			clients, err := tmux.Run("list-clients", "-F", "#{client_name}")
			if err != nil || clients == "" {
				return fmt.Errorf("no tmux clients found")
			}
			// Use the first (most recent) client
			client := clients[:strings.Index(clients+"\n", "\n")]
			_, err = tmux.Run("switch-client", "-c", client, "-t", session)
			return err
		},
	}

	// agmux init
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Setup wizard — configure hooks for your agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			m := ui.NewInitModel()
			p := tea.NewProgram(m)
			_, err := p.Run()
			return err
		},
	}

	rootCmd.AddCommand(popupCmd, toggleCmd, notifyCmd, clearCmd, statusCmd, switchCmd, initCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func togglePopup(width, height string) error {
	// Auto-size height to content, capped at 80% terminal height
	autoHeight := calcPopupHeight()
	if autoHeight > 0 {
		height = fmt.Sprintf("%d", autoHeight)
	}

	_, err := tmux.Run(
		"display-popup",
		"-E",
		"-w", width,
		"-h", height,
		"-b", "rounded",
		"-S", "fg=#c0c0c0",
		"-T", " ⬡ agents ",
		"agmux", "popup",
	)
	if err != nil {
		// Fallback: run the TUI inline when display-popup fails
		// (e.g. iTerm2 tmux integration, no client context)
		m := ui.NewModel()
		p := tea.NewProgram(m, tea.WithAltScreen())
		_, err = p.Run()
	}
	return err
}

func calcPopupHeight() int {
	sessions, err := tmux.ListSessions()
	if err != nil {
		return 0
	}

	lines := 6 // summary + separator + footer + border chrome + padding
	for _, s := range sessions {
		wins, _ := tmux.ListWindowsWithPanes(s.Name)
		lines++ // session name line

		totalAgents := 0
		windowsWithAgents := 0
		for _, w := range wins {
			n := 0
			for _, p := range w.Panes {
				if name, _ := tmux.DetectAgent(p); name != "" {
					n++
				}
			}
			if n > 0 {
				windowsWithAgents++
				totalAgents += n
			}
		}

		if totalAgents <= 1 {
			lines++ // compact detail line
		} else {
			lines += totalAgents // flat list, one per agent
		}

		lines++ // blank separator between sessions
	}

	if lines < 8 {
		lines = 8
	}

	// Cap at 80% of terminal
	termHeight := 50
	if out, err := tmux.Run("display-message", "-p", "#{window_height}"); err == nil {
		fmt.Sscanf(out, "%d", &termHeight)
	}
	if maxH := termHeight * 80 / 100; lines > maxH {
		lines = maxH
	}

	return lines
}
