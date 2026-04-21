# Claudster

A terminal UI for managing multiple Claude sessions across projects.

<img width="2553" height="1520" alt="CleanShot 2026-04-20 at 10 26 54" src="https://github.com/user-attachments/assets/2ab3e880-aead-4748-8c18-f4b1bf963fda" />

## Prerequisites

- [tmux](https://github.com/tmux/tmux/wiki/Installing) — claudster runs everything inside tmux sessions
- [Claude CLI](https://docs.anthropic.com/en/docs/claude-code) — `claude` must be on your PATH

## Installation

Download the binary for your platform from the [releases page](../../releases) and put it on your PATH:

```bash
# macOS (Apple Silicon)
curl -L https://github.com/yourname/claudster/releases/latest/download/claudster-darwin-arm64 -o claudster

# macOS (Intel)
curl -L https://github.com/yourname/claudster/releases/latest/download/claudster-darwin-amd64 -o claudster

# Linux / WSL
curl -L https://github.com/yourname/claudster/releases/latest/download/claudster-linux-amd64 -o claudster

chmod +x claudster
sudo mv claudster /usr/local/bin/
```

## Setup

Claudster reads its config from `~/.claudster.yaml`. Create it by copying the example:

```bash
cp claudster.example.yaml ~/.claudster.yaml
```

Then edit it to point at your actual repos:

```yaml
groups:
  - name: work
    projects:
      - name: my-app
        repos:
          - ~/code/my-app        # primary repo — sessions start here
          - ~/code/my-app-api    # additional repos passed to Claude via --add-dir
        sessions:
          - name: feature-x
```

Run claudster **inside tmux**:

```bash
tmux
claudster
```

## Keybindings

| Key | Action |
|-----|--------|
| `enter` | Attach to session (starts it if not running) |
| `n` | New Claude session |
| `r` | Resume a previous Claude session (picker) |
| `d` | Delete session |
| `P` | Restart session |
| `v` | Open repo in editor |
| `t` | Open repo in terminal popup |
| `G` | Open repo in lazygit |
| `N` | New project |
| `e` | Edit config file |
| `[ / ]` | Resize sidebar |
| `?` | Help |
| `q` | Quit |

When a Claude session finishes, a toast notification appears. Press `1`–`9` to jump to it, or `Alt+1`–`Alt+9` from any tmux session.

## Building from source

Requires Go 1.21+.

```bash
make build        # current platform
make release      # all platforms → dist/
make install      # build + install to /usr/local/bin
```
