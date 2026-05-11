# CLIP — CLaude Interactive Picker

Quickly find and resume Claude Code and Codex conversations.

- ⚡ Full-text search across your Claude and Codex session history
- 👀 Inline preview of past conversations
- ▶️ Hit Enter to pick up right where you left off

![screenshot](screenshot.png)

## Install

```bash
go install github.com/magicmark/clip@latest
```

...or download a binary from [GitHub Releases](https://github.com/magicmark/clip/releases):

```bash
# macOS (Apple Silicon)
curl -L -o clip "https://github.com/magicmark/clip/releases/latest/download/clip-darwin-arm64"
chmod +x clip && mv clip /usr/local/bin/

# Linux (x86_64)
curl -L -o clip "https://github.com/magicmark/clip/releases/latest/download/clip-linux-amd64"
chmod +x clip && mv clip /usr/local/bin/
```

## Configuration

Clip reads optional settings from `~/.config/clip.toml`:

```toml
ignore_directories = [
  "/tmp/private-*",
]

claude_startup_flags = "--dangerously-skip-permissions"
codex_startup_flags = "--sandbox workspace-write --ask-for-approval on-request"
```

`ignore_directories` applies to both Claude and Codex sessions. Startup flags are source-specific: Claude flags are only appended to `claude --resume`, and Codex flags are only appended to `codex resume`.

## Prior Art + Acknowledgments

Inspired by https://github.com/angristan/fast-resume

## License

MIT
