# agmux

Agent orchestration for tmux. Manage AI coding agents across sessions with a popup switcher, live status detection, and macOS notifications.

Works with Claude Code, Codex, Kiro, Aider, Goose, or any CLI agent.

![agmux demo](demo.gif)

## Install

```bash
brew install trentkm/tap/agmux
```

Or build from source:

```bash
go build -o ~/.local/bin/agmux .
```

## What it does

**Popup switcher** — press a keybind to open a floating popup showing all your tmux sessions with live agent status. Navigate with j/k, Enter to switch, / to search.

**Live agent detection** — detects running agents by process name and pane title. Shows working/idle status per pane.

**Notifications** — agents report their status via hooks. "Waiting" means the agent needs your input. "Done" means it finished. macOS notifications fire with click-to-switch.

**Status bar** — shows per-session agent status in your tmux footer. Auto-compacts when you have many sessions.

## Setup

### 1. Tmux keybind

Add to your `~/.tmux.conf`:

```bash
# Agent popup (prefix+b to toggle)
bind b run-shell 'agmux toggle'

# Status bar widget
set -g status-right '#(agmux status) %H:%M'
set -g status-interval 2
```

Then reload: `tmux source-file ~/.tmux.conf`

### 2. Agent hooks

#### Claude Code

Add to `~/.claude/settings.json`:

```json
{
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "agmux notify --status working",
            "timeout": 5
          }
        ]
      }
    ],
    "Notification": [
      {
        "matcher": "permission_prompt",
        "hooks": [
          {
            "type": "command",
            "command": "agmux notify --status waiting",
            "timeout": 5
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "agmux notify --status done",
            "timeout": 5
          }
        ]
      }
    ]
  }
}
```

- **`UserPromptSubmit`** — fires when you send a prompt. Shows "working" status.
- **`Notification` (permission_prompt)** — fires when Claude is blocked on a permission prompt. Shows "waiting" status.
- **`Stop`** — fires when Claude finishes its response. Shows "done" status.

#### Kiro CLI

Add hooks to a [custom agent config](https://kiro.dev/docs/cli/custom-agents/configuration-reference#hooks-field). Create or edit `~/.kiro/agents/default.json`:

```json
{
  "name": "default",
  "hooks": {
    "agentSpawn": [
      {
        "command": "agmux notify --status done"
      }
    ],
    "userPromptSubmit": [
      {
        "command": "agmux notify --status working"
      }
    ],
    "stop": [
      {
        "command": "agmux notify --status done"
      }
    ]
  }
}
```

- **`agentSpawn`** — fires when Kiro starts. Registers the session in agmux immediately.
- **`userPromptSubmit`** — fires when you send a prompt. Shows "working" status.
- **`stop`** — fires when Kiro finishes its response. Shows "done" status.

Then start Kiro CLI with: `kiro-cli chat --agent default`

Or add the `hooks` block to any existing agent config you already use.

#### Other agents

Any agent that can run shell commands on lifecycle events can integrate with agmux:

```bash
# When agent starts processing:
agmux notify --status working

# When agent needs user input:
agmux notify --status waiting

# When agent finishes a task:
agmux notify --status done

# Optionally specify which session:
agmux notify --status done --session my-project
```

### 3. macOS notifications (optional)

Install `terminal-notifier` for clickable notifications that switch to the session:

```bash
brew install terminal-notifier
```

Without it, agmux falls back to basic macOS notifications via osascript (no click-to-switch).

## Usage

| Command | Description |
|---------|-------------|
| `agmux toggle` | Open/close the agent popup |
| `agmux status` | Status bar widget (for tmux status-right) |
| `agmux notify --status working` | Mark session as agent working |
| `agmux notify --status waiting` | Mark session as waiting for input |
| `agmux notify --status done` | Mark session as task complete |
| `agmux clear [session]` | Clear notification for a session |
| `agmux clear --all` | Clear all notifications |

### Popup keys

| Key | Action |
|-----|--------|
| `j/k` | Navigate |
| `Enter` | Switch to session |
| `/` | Search/filter sessions |
| `c` | Clear notification for selected session |
| `C` | Clear all notifications |
| `q` / `Esc` | Close popup |
| `:q` | Vim-style quit |

## Status indicators

| Indicator | Meaning |
|-----------|---------|
| `░▒▓█` (pulsing) | Agent actively working |
| `● waiting` | Agent needs your response |
| `✓ done` | Agent finished its task |
| `·` | Agent idle |

## How it works

- **Live detection** — reads tmux pane titles and process names every 2 seconds to detect running agents
- **Notification state** — stored as files in `~/.local/state/agmux/` (one per session)
- **Status priority** — working (spinner detected) > waiting > done
- **macOS notifications** — fired via `terminal-notifier` (with click-to-switch) or `osascript` (basic)
