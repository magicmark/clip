## Why

Clip can search and resume Claude Code conversations today, but Codex users have separate session history under `~/.codex/sessions` that is not discoverable from the app. Adding Codex session search lets the same picker and CLI workflow find recent work across both assistants without switching tools.

## What Changes

- Add support for discovering Codex JSONL session files from the Codex session store.
- Parse Codex session metadata and message records into the existing session/search model.
- Include Codex sessions in full-text search, previews, dates, turns, and ignored-directory filtering.
- Resume selected Codex sessions with `codex resume <session-id>` while preserving Claude resume behavior for Claude sessions.
- Add `codex_startup_flags` to `~/.config/clip.toml` for optional Codex resume flags, while keeping `ignore_directories` source-neutral and `claude_startup_flags` Claude-only.
- Show enough source information in rows, previews, and CLI output to distinguish Claude and Codex sessions when needed.

## Capabilities

### New Capabilities

- `codex-session-search`: Users can search, preview, and resume Codex sessions alongside existing Claude sessions.

### Modified Capabilities

- None.

## Impact

- Affected code: session loading/parsing in `main.go`, search indexing and fallback matching, resume command construction, table/preview labels, search CLI output in `search_cli.go`, and related tests.
- Affected config: `~/.config/clip.toml` gains optional `codex_startup_flags`; existing `ignore_directories` applies to both Claude and Codex session working directories; existing `claude_startup_flags` remains Claude-only.
- Affected data sources: adds read-only access to `~/.codex/sessions/**/*.jsonl` in addition to existing `~/.claude/projects/**/*.jsonl`.
- Affected commands: interactive resume dispatch must choose `claude --resume` or `codex resume` based on session source; CLI output must print the matching resume command for each source.
- Dependencies: no new external dependency is expected; existing JSON parsing, filepath walking, and Bleve indexing should be sufficient.
