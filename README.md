# Claudster

A terminal UI for managing multiple Claude sessions across projects.

<img width="2553" height="1520" alt="CleanShot 2026-04-20 at 10 26 54" src="https://github.com/user-attachments/assets/2ab3e880-aead-4748-8c18-f4b1bf963fda" />

## Prerequisites

- [tmux](https://github.com/tmux/tmux/wiki/Installing) — claudster runs everything inside tmux sessions
- [Claude CLI](https://docs.anthropic.com/en/docs/claude-code) — `claude` must be on your PATH

---

## Install

Download and install the binary for your platform.

### macOS (Apple Silicon)
```bash
curl -L https://github.com/jonagull/claudster/releases/latest/download/claudster-darwin-arm64 -o claudster
chmod +x claudster
sudo mv claudster /usr/local/bin/
```

### macOS (Intel)
```bash
curl -L https://github.com/jonagull/claudster/releases/latest/download/claudster-darwin-amd64 -o claudster
chmod +x claudster
sudo mv claudster /usr/local/bin/
```

### Linux / WSL
```bash
curl -L https://github.com/jonagull/claudster/releases/latest/download/claudster-linux-amd64 -o claudster
chmod +x claudster
mkdir -p ~/.local/bin
mv claudster ~/.local/bin/
```

Then open a new terminal and verify:
```bash
claudster --help
```

> **WSL:** If you get "command not found", add this to your `~/.bashrc` and reopen the terminal:
> ```bash
> export PATH="$HOME/.local/bin:$PATH"
> ```

### Updating

Same steps as install — just re-run the curl command for your platform and overwrite the old binary. Your config (`~/.claudster.yaml`) is untouched.

---

## Getting started

### 1. Start tmux and launch claudster

Claudster must run inside tmux. If you're new to tmux, think of it as a window manager for your terminal — claudster lives in one window and your Claude sessions each get their own.

```bash
tmux new-session -s claudster -d 'claudster' && tmux attach -t claudster
```

If you're already inside tmux:
```bash
claudster
```

### 2. Edit your config

On first run, claudster creates `~/.claudster.yaml` automatically. Press `e` to open it and point it at your repos:

```yaml
groups:
  - name: work
    projects:
      - name: my-app
        repos:
          - ~/code/my-app        # primary repo — sessions start here
          - ~/code/my-app-api    # additional repos, passed to Claude via --add-dir
        sessions: []
```

Press `N` to add a new project interactively without editing the file directly.

### 3. Start a session

Select a project in the sidebar and press `n` to start a new Claude session, or `r` to resume a previous one.

---

## Editor

Claudster picks your editor in this order:
1. `ui.editor` in config
2. `$EDITOR` environment variable
3. Auto-detected from PATH: `code` → `nano` → `vim` → `vi`

To pin a specific editor, add it to your config:

```yaml
ui:
  editor: code   # or nvim, vim, nano, etc.
```

**VS Code / WSL:** `code` is fully supported. Claudster handles paths correctly so files open in your WSL filesystem, not the Windows side. The one exception is `V` (persistent editor session in tmux) — VS Code can't run inside tmux, use `v` to open the folder instead.

> WSL tip: install the VS Code [Remote - WSL](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-wsl) extension, then run `code .` once from your terminal to register `code` on the PATH.

---

## Git client

By default `G` opens lazygit in the terminal. You can switch to GitHub Desktop:

```yaml
ui:
  git_client: github-desktop
```

- **macOS** — works out of the box
- **Windows / WSL** — requires the `github` CLI helper on your PATH, which GitHub Desktop installs automatically

---

## Keybindings

| Key | Action |
|-----|--------|
| `enter` | Attach to session (starts it if not running) |
| `n` | New Claude session |
| `r` | Resume a previous Claude session (picker) |
| `d` | Delete session |
| `P` | Restart session |
| `v` | Open repo in editor |
| `V` | New persistent editor session (terminal editors only) |
| `t` | Open repo in terminal popup |
| `G` | Open repo in git client |
| `N` | New project |
| `e` | Edit config file |
| `p` | Toggle `--dangerously-skip-permissions` |
| `[ / ]` | Resize sidebar |
| `?` | Full keybinding help |
| `q` | Quit |

When creating a new session (`n` or `r`) you'll be asked whether to run with `--dangerously-skip-permissions` before it starts.

### Notifications

When a Claude session finishes, a toast appears in the corner. If you're in another tmux session at the time, a notification also flashes in your tmux status bar.

- Press `1`–`9` in claudster to jump to that session
- Press `opt+1`–`opt+9` (Mac) or `alt+1`–`alt+9` (Linux/WSL) from anywhere in tmux to jump directly

> **Mac:** For `opt+N` to work, set Left Option Key to `Esc+` in iTerm2: Preferences → Profiles → Keys.

---

## Build from source

Requires Go 1.26+.

```bash
git clone https://github.com/jonagull/claudster.git
cd claudster
make build        # build for current platform → ./claudster
make install      # build + install to /usr/local/bin
make release      # build all platforms → dist/
```
