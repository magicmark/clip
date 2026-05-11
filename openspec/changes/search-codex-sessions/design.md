## Context

Clip currently loads Claude Code JSONL transcripts from `~/.claude/projects`, parses them into a single `session` type, indexes them with Bleve, renders a table and conversation preview, and resumes selected sessions with `claude --resume <id>`. The CLI `-s/--search` path reuses the same loaded sessions and prints a resumable command for each match.

Codex stores interactive sessions separately under `~/.codex/sessions/<year>/<month>/<day>/*.jsonl`. These JSONL records include `session_meta` entries with an `id`, `cwd`, and timestamp; message records appear as `response_item` payloads with `type: "message"` and user/assistant roles, plus event messages such as `user_message` and `agent_message` in some records.

## Goals / Non-Goals

**Goals:**

- Load Claude and Codex sessions into one source-aware session list.
- Search Codex titles, directories, paths, and message bodies through the same full-text and fallback search behavior as Claude sessions.
- Preview Codex user and assistant turns in the existing viewer.
- Resume Codex sessions with `codex resume <session-id>` while preserving existing Claude resume behavior.
- Keep Clip config backward-compatible while adding optional Codex startup flags and applying ignored-directory filtering across both sources.
- Make session source visible enough that mixed Claude/Codex results are understandable.

**Non-Goals:**

- Replace Codex's built-in resume picker.
- Add write, delete, rename, or transcript migration behavior for Codex files.
- Index tool-call payloads, command output, reasoning items, or encrypted content as conversation text.
- Add custom session-store paths, source enable/disable toggles, or per-source ignore lists.
- Add a new external search or storage dependency.

## Decisions

### Use a source-aware shared session model

Add a source field to the existing `session` model, with values such as `claude` and `codex`, and keep common fields for id, path, title, directory, turns, date, and messages. Search, table rendering, preview rendering, and CLI output should operate over this unified model.

Rationale: the existing UI and search behavior already work for assistant transcripts once they are normalized. A shared model avoids duplicating table/search code and keeps mixed results sorted by date.

Alternative considered: create a separate Codex-only mode. That would be simpler to parse but would make search and resume workflows less useful because users would need to choose a source before searching.

### Keep raw session id separate from search identity

Introduce a stable search/document key derived from source and raw id, for example `codex:<uuid>` and `claude:<uuid>`. Use that key for Bleve document IDs and matched-ID maps, while preserving the raw id for resume commands.

Rationale: Claude and Codex ids are both session identifiers, and although collisions are unlikely, the search index and filtering logic need a guaranteed unique key across sources.

Alternative considered: prefix the existing `id` field. That risks leaking prefixed ids into resume commands and recovery messages that require the original tool-specific id.

### Parse Codex JSONL conservatively

Add a Codex parser that reads JSONL records and extracts:

- `session_meta.payload.id` as the raw Codex session id.
- `session_meta.payload.cwd` as the preferred directory, with `turn_context.payload.cwd` as a fallback.
- record timestamps and file modification time for sorting.
- user and assistant conversation text from `response_item` message payloads.
- event `user_message` and `agent_message` text only as a fallback or when it adds non-duplicate visible conversation text.

Ignore tool calls, command output, reasoning records, token counts, encrypted content, and other operational events for the conversation preview and index. Also remove Codex-injected synthetic XML blocks such as `<environment_context>`, `<skill>`, and `<turn_aborted>` before computing the session title, turn count, preview, and indexed/searchable body.

Rationale: Codex transcripts contain both conversation and execution telemetry. Search results should match user-visible conversation history, not every command output line or tool payload.

Alternative considered: index every textual field in the JSONL. That would increase recall but produce noisy matches from shell output, internal events, and implementation details that were never part of the chat.

### Exclude source-generated synthetic messages

Exclude source-generated synthetic transcript messages from the normalized session model before computing titles, turn counts, previews, and searchable content. For Claude sessions, skip task notification messages such as `<task-notification>...</task-notification>` records, including newer records marked with `origin.kind: "task-notification"` and older records that only expose the XML wrapper in message content. For Codex sessions, strip injected XML blocks such as `<environment_context>`, `<skill>`, and `<turn_aborted>`.

Rationale: these records are assistant tooling metadata rather than conversation turns. Including them makes search results noisy and can cause previews to show task IDs, output file paths, or environment details instead of the user-visible conversation.

### Combine sources during load

Split loading into source-specific helpers, then have `loadSessions()` aggregate Claude and Codex sessions, apply existing ignored-directory filtering, and sort the combined list by descending date.

Rationale: most existing callers already use `loadSessions()` and should automatically see the combined behavior.

Alternative considered: add a second top-level `loadCodexSessions()` caller in the UI and CLI. That would spread source-combining logic across the codebase and increase the chance of inconsistent filtering or sorting.

### Keep `clip.toml` source-aware only for resume flags

Keep using `~/.config/clip.toml` as the only Clip config file. The new config shape is:

```toml
ignore_directories = ["/abs/path", "/tmp/work-*"]
claude_startup_flags = "--dangerously-skip-permissions"
codex_startup_flags = "--sandbox workspace-write --ask-for-approval on-request"
```

`ignore_directories` remains source-neutral and applies to the normalized working directory for both Claude and Codex sessions. `claude_startup_flags` remains Claude-only. Add `codex_startup_flags` as an optional string that is appended only to Codex resume commands. Missing or empty startup flag strings append nothing. Use the same string-splitting behavior for Codex flags that the existing implementation uses for Claude flags.

Rationale: users already have one Clip config file and one ignore list. Source-specific startup flags are the only new config needed because the resume commands are source-specific.

Alternative considered: add nested per-source config sections such as `[claude]` and `[codex]`. That would be cleaner long term, but it would be a larger config migration than this change needs.

### Surface source in UI and CLI output

Add a compact source indicator to session rows and CLI output. The table can use a short `Source` column or a title prefix if space is constrained; CLI output should include a source label near the directory and command.

Source labels are display metadata, not searchable conversation metadata. Do not index the displayed `Claude` or `Codex` label, and do not index role/source prefixes such as `Claude:` or `Codex:` as part of message content. Transcript path matching should also ignore source store roots such as `~/.claude/projects` and `~/.codex/sessions` so a query for `claude` or `codex` does not match every session from that source. When rendering CLI snippets, labels such as `Claude:`, `Codex:`, `Title:`, or `Path:` may orient the user but should not themselves be highlighted as the matched text.

Rationale: once results contain both Claude and Codex history, users need to know which tool will be launched before they press Enter or copy a resume command.

Alternative considered: infer source from the command only. That is less visible in the interactive table and easy to miss.

### Dispatch resume by source

Use source-specific resume command construction:

- Claude: keep `claude --resume <id>` plus existing Claude startup flags.
- Codex: run `codex resume <id>` plus optional Codex startup flags.

When a session has a valid directory, keep the current behavior of launching from that directory. For CLI output, print `cd <dir> && codex resume <id> <codex_startup_flags>` for Codex sessions with a directory and configured Codex flags.

Rationale: both tools restore sessions by raw id, and launching from the recorded working directory preserves the current Clip workflow.

Alternative considered: always use `codex resume --all <id>` or `codex resume -C <dir> <id>`. The local CLI accepts raw ids directly, and the existing app already expresses working directory by process cwd and shell `cd`.

### Rebuild the index for source-aware keys

Bump `indexVersion` when adding source-aware document keys and any new indexed source field. Existing indexes should be discarded and rebuilt from local transcript files.

Rationale: old document ids are not source-qualified, so mixed-source results would otherwise be inconsistent.

Alternative considered: migrate existing index documents in place. Rebuilding is simpler and safe because the index is derived cache data.

## Risks / Trade-offs

- Codex transcript format changes -> keep parser tolerant of missing fields, use file path and file modification time fallbacks, and add tests for representative record shapes.
- Duplicate text from Codex event and response records -> prefer canonical `response_item` messages and deduplicate fallback event messages.
- Mixed-source index keys require touching search plumbing -> isolate key generation in a helper and cover filtering/index tests.
- Startup flags can leak across sources if command construction is not isolated -> cover Claude-only and Codex-only flag behavior in tests.
- Large Codex histories increase startup indexing work -> retain existing incremental mod-time checks and avoid indexing operational payloads.
- `codex` binary missing when resuming -> surface the process failure naturally without affecting search and preview behavior.

## Migration Plan

1. Add source-aware fields and helpers while preserving existing Claude behavior.
2. Extend config loading with optional `codex_startup_flags` while preserving existing config defaults.
3. Add Codex discovery and parsing behind the shared loader.
4. Update search document keys and bump the index version so the cache rebuilds.
5. Update UI/CLI source labels and resume command construction.
6. Add tests for Claude regression behavior, Codex parsing, mixed-source search, ignored directories, and source-specific resume commands.

Rollback is to remove Codex loading and restore the previous index version/key behavior; no user transcript data is modified by this change.
