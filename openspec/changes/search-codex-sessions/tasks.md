## 1. Source-Aware Session Model

- [x] 1.1 Add a session source field and helpers for display labels, search document keys, and source-specific raw ids.
- [x] 1.2 Update existing Claude session parsing to set the Claude source without changing current title, directory, date, message, or turn behavior.
- [x] 1.3 Update table row generation and selected-session lookup to work with source-aware sessions.

## 2. Codex Session Discovery And Parsing

- [x] 2.1 Add Codex session discovery under `~/.codex/sessions/**/*.jsonl` with tolerant handling for missing or unreadable directories.
- [x] 2.2 Implement Codex JSONL parsing for session metadata, cwd fallback, timestamps, user/assistant messages, titles, turn counts, and file path.
- [x] 2.3 Ignore Codex operational records such as tool calls, command output, reasoning, token counts, and encrypted content.
- [x] 2.4 Strip Codex-injected synthetic XML blocks such as `<environment_context>`, `<skill>`, and `<turn_aborted>` from title, preview, turn counts, and search content.
- [x] 2.5 Ignore Claude task notification messages such as `<task-notification>` and `origin.kind: "task-notification"` before title, preview, turn counts, and search content.
- [x] 2.6 Merge Claude and Codex sessions in `loadSessions()`, apply ignored-directory filtering, and sort the combined list by most recent activity.

## 3. Search And Indexing

- [x] 3.1 Update Bleve indexing and deletion to use source-qualified document keys while preserving raw ids for resume commands.
- [x] 3.2 Include Codex sessions in indexed and loaded fallback search across title, directory, path, id, and message content.
- [x] 3.3 Bump the search index version so existing cached indexes rebuild with source-aware keys.
- [x] 3.4 Verify matched-term highlighting still works for Claude and Codex search results.

## 4. UI, CLI, And Resume Behavior

- [x] 4.1 Add a compact source indicator to interactive table rows without breaking responsive column sizing.
- [x] 4.2 Add a source label to CLI search output.
- [x] 4.3 Build source-specific resume commands for interactive launch and CLI output.
- [x] 4.4 Add optional `codex_startup_flags` config support and append those flags only to Codex resume commands.
- [x] 4.5 Preserve existing `claude_startup_flags` behavior and ensure Claude flags are not applied to Codex sessions.
- [x] 4.6 Keep the missing-directory recovery message useful for both Claude and Codex sessions.
- [x] 4.7 Align CLI search metadata indentation with each numbered result title.

## 5. Tests And Verification

- [x] 5.1 Add Codex parser tests with representative `session_meta`, `turn_context`, `response_item` message, and operational event records.
- [x] 5.2 Add mixed Claude/Codex search tests covering body matches, source-qualified key collisions, and ignored directories.
- [x] 5.3 Add CLI output tests for Codex source labels and `codex resume` commands with and without configured Codex startup flags.
- [x] 5.4 Add config tests showing `ignore_directories` applies to both sources, `codex_startup_flags` is optional, and Claude/Codex startup flags remain source-specific.
- [x] 5.5 Add regression tests showing Codex synthetic XML blocks are stripped before title, preview, turn count, and search handling.
- [x] 5.6 Add regression tests showing Claude task notifications are skipped before title, preview, turn count, and search handling.
- [x] 5.7 Add regression tests showing Claude parsing, search, CLI output, and resume commands still behave as before.
- [x] 5.8 Add regression tests showing displayed source labels and source storage roots do not create search matches.
- [x] 5.9 Add regression tests showing CLI search metadata aligns under double-digit result titles.
- [x] 5.10 Add regression tests showing CLI snippet display labels are not highlighted as matches.
- [x] 5.11 Run the Go test suite and fix any regressions.
