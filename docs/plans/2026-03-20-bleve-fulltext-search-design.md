# Full-Text Search with Bleve

## Summary

Replace the current title-only substring filter with full-text search across entire conversation threads using Bleve (the embeddable Go search engine that powers ZincSearch). Highlight matched terms in the conversation preview pane.

## Decisions

- **Search engine:** Bleve (`github.com/blevesearch/bleve/v2`) — embedded, no external server
- **Index location:** `~/.cache/clips/search.index`
- **Index strategy:** Persistent on disk with incremental updates (compare file mod times)
- **UI behavior:** Table shows matching sessions (same columns as now); matched terms highlighted in preview pane only
- **Highlight style:** Yellow/gold background on matched terms
- **Search trigger:** Real-time, as-you-type (same as current behavior)

## Architecture

### Indexing

Each indexed document represents one session with fields:
- `id` — session ID (filename without extension)
- `title` — first user message (truncated)
- `directory` — project directory
- `content` — all messages concatenated with role labels ("You: ...\nClaude: ...")
- `path` — JSONL file path
- `mod_time` — file modification time (stored for incremental sync)

On startup:
1. Open or create Bleve index at `~/.cache/clips/search.index`
2. Walk all JSONL session files
3. For each file, compare mod time against indexed value
4. Index new/changed sessions, remove deleted ones
5. This runs before the TUI starts (blocking but fast after first run)

### Search Flow

1. User types in search bar
2. On each keystroke, execute Bleve query (match query across title + content fields)
3. Bleve returns matching session IDs and matched terms
4. Table filters to show only matching sessions
5. When a session is selected, render conversation with yellow background highlighting on matched terms

### Highlighting

- Extract matched terms/fragments from Bleve search results
- When rendering the conversation preview, case-insensitive scan for those terms
- Wrap matched substrings with Lip Gloss yellow background style
- When search query is empty, render normally (no highlighting)

### What Changes

- Add `github.com/blevesearch/bleve/v2` dependency
- New functions: `openOrCreateIndex()`, `syncIndex()`, `searchSessions()`
- Modify `filterRows()` to use Bleve instead of substring matching
- Modify conversation preview renderer to highlight matched terms
- Add `matchedTerms []string` field to the model

### What Stays the Same

- All UI layout, navigation, keybindings
- Session loading from JSONL files
- Table columns and styling
- Enter to resume a session
