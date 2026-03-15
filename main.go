package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/trent/tmux-workspace/internal/notify"
	"github.com/trent/tmux-workspace/internal/tmux"
	"github.com/trent/tmux-workspace/internal/ui"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "tw",
		Short: "tmux workspace manager",
		Long:  "A workspace manager for tmux with notifications and a visual sidebar.",
	}

	// tw popup — run the TUI directly (called by display-popup)
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

	// tw toggle — open or dismiss the popup
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

	// tw notify --status waiting|done
	notifyCmd := &cobra.Command{
		Use:   "notify",
		Short: "Send a notification for the current tmux session",
		RunE: func(cmd *cobra.Command, args []string) error {
			session, _ := cmd.Flags().GetString("session")
			if session == "" {
				session = tmux.CurrentSession()
				if session == "" {
					session = "default"
				}
			}
			status, _ := cmd.Flags().GetString("status")
			switch status {
			case "waiting", "done":
				return notify.Add(session, notify.Status(status))
			default:
				return fmt.Errorf("--status must be 'waiting' or 'done'")
			}
		},
	}
	notifyCmd.Flags().StringP("session", "s", "", "Session name (default: current tmux session)")
	notifyCmd.Flags().String("status", "", "Notification status: waiting or done")

	// tw clear
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

	// tw status
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Status bar widget (for tmux status-right)",
		Run: func(cmd *cobra.Command, args []string) {
			waiting := notify.CountByStatus(notify.StatusWaiting)
			done := notify.CountByStatus(notify.StatusDone)
			if waiting > 0 {
				fmt.Printf("#[fg=yellow,bold] ● %d agent waiting #[default]", waiting)
			} else if done > 0 {
				fmt.Printf("#[fg=green] ✓ %d agent done #[default]", done)
			}
		},
	}

	// tw switch <session> — switch to a session (works from outside tmux)
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

	rootCmd.AddCommand(popupCmd, toggleCmd, notifyCmd, clearCmd, statusCmd, switchCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func togglePopup(width, height string) error {
	// display-popup natively toggles: if a popup with the same -T title
	// is already open, calling display-popup again closes it.
	// This means prefix+b opens the popup, and prefix+b again closes it.
	_, err := tmux.Run(
		"display-popup",
		"-E",
		"-w", width,
		"-h", height,
		"-b", "rounded",
		"-S", "fg=#c0c0c0",
		"-T", " ⬡ agents ",
		"tw", "popup",
	)
	return err
}
