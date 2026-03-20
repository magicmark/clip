# CLIP — CLaude Interactive Picker

Quickly find and resume any Claude Code conversation.

- ⚡ Full-text search across your entire session history
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

## Prior Art + Acknowledgments

Inspired by https://github.com/angristan/fast-resume

## License

MIT
