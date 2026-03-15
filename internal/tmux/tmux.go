package tmux

import (
	"os/exec"
	"strconv"
	"strings"
)

type Session struct {
	Name     string
	Windows  int
	Attached int
}

type Window struct {
	Index  int
	Name   string
	Active bool
	Panes  []Pane
}

type Pane struct {
	Index   int
	Command string
	Path    string
	Title   string
	Active  bool
}

func Run(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func CurrentSession() string {
	out, err := Run("display-message", "-p", "#S")
	if err != nil {
		return ""
	}
	return out
}

// ClientSession returns the session the most recently active client is viewing.
// This differs from CurrentSession() when the sidebar pane belongs to a
// different session than the one the user is looking at (after switch-client).
func ClientSession() string {
	out, err := Run("list-clients", "-F", "#{client_activity}|#{client_session}")
	if err != nil {
		return CurrentSession()
	}
	// Find most recently active client
	var bestSession string
	var bestActivity string
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[0] > bestActivity {
			bestActivity = parts[0]
			bestSession = parts[1]
		}
	}
	if bestSession == "" {
		return CurrentSession()
	}
	return bestSession
}

func ListSessions() ([]Session, error) {
	out, err := Run("list-sessions", "-F", "#{session_name}|#{session_windows}|#{session_attached}")
	if err != nil {
		return nil, err
	}

	var sessions []Session
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		wins, _ := strconv.Atoi(parts[1])
		attached, _ := strconv.Atoi(parts[2])
		sessions = append(sessions, Session{
			Name:     parts[0],
			Windows:  wins,
			Attached: attached,
		})
	}
	return sessions, nil
}

// ListWindowsWithPanes returns all windows for a session, each with their panes.
func ListWindowsWithPanes(session string) ([]Window, error) {
	out, err := Run("list-panes", "-t", session, "-F",
		"#{window_index}|#{window_name}|#{window_active}|#{pane_index}|#{pane_current_command}|#{pane_current_path}|#{pane_title}|#{pane_active}")
	if err != nil {
		return nil, err
	}

	windowMap := make(map[int]*Window)
	var windowOrder []int

	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 8)
		if len(parts) != 8 {
			continue
		}
		winIdx, _ := strconv.Atoi(parts[0])
		paneIdx, _ := strconv.Atoi(parts[3])

		w, ok := windowMap[winIdx]
		if !ok {
			w = &Window{
				Index:  winIdx,
				Name:   parts[1],
				Active: parts[2] == "1",
			}
			windowMap[winIdx] = w
			windowOrder = append(windowOrder, winIdx)
		}

		w.Panes = append(w.Panes, Pane{
			Index:   paneIdx,
			Command: parts[4],
			Path:    parts[5],
			Title:   parts[6],
			Active:  parts[7] == "1",
		})
	}

	var windows []Window
	for _, idx := range windowOrder {
		windows = append(windows, *windowMap[idx])
	}
	return windows, nil
}

func SwitchClient(session string) error {
	_, err := Run("switch-client", "-t", session)
	return err
}

func SplitWindow(args ...string) error {
	a := append([]string{"split-window"}, args...)
	_, err := Run(a...)
	return err
}

func KillPane(paneID string) error {
	_, err := Run("kill-pane", "-t", paneID)
	return err
}

// IsShell returns true if the command is a common shell.
func IsShell(cmd string) bool {
	switch cmd {
	case "fish", "bash", "zsh", "sh", "dash", "tcsh", "csh":
		return true
	}
	return false
}
