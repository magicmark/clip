## ADDED Requirements

### Requirement: Discover Codex Sessions
The system SHALL discover Codex JSONL session transcripts stored under `~/.codex/sessions/**/*.jsonl` and include valid Codex sessions in the same session list as existing Claude sessions.

#### Scenario: Codex session appears in combined results
- **WHEN** a Codex JSONL file contains session metadata with an id, cwd, timestamp, and at least one user-visible message
- **THEN** the system lists the Codex session together with existing Claude sessions sorted by most recent activity

#### Scenario: Missing Codex store
- **WHEN** `~/.codex/sessions` does not exist or cannot be read
- **THEN** the system continues to list searchable Claude sessions without failing startup

#### Scenario: Ignored Codex directory
- **WHEN** a Codex session's recorded working directory matches an ignored directory pattern
- **THEN** the system excludes that Codex session from the combined session list

### Requirement: Configure Multi-Source Session Behavior
The system SHALL keep using `~/.config/clip.toml` for Clip configuration while supporting source-neutral ignore patterns and source-specific startup flags.

#### Scenario: Source-neutral ignored directories
- **GIVEN** `~/.config/clip.toml` contains `ignore_directories`
- **WHEN** Claude or Codex sessions have recorded working directories matching those patterns
- **THEN** the system excludes matching sessions from the combined session list regardless of source

#### Scenario: Optional Codex startup flags
- **GIVEN** `~/.config/clip.toml` contains `codex_startup_flags`
- **WHEN** the user resumes a Codex session interactively or copies a Codex command from CLI search output
- **THEN** the system appends those flags to `codex resume <session-id>`

#### Scenario: Source-specific startup flags
- **GIVEN** `~/.config/clip.toml` contains both `claude_startup_flags` and `codex_startup_flags`
- **WHEN** the system builds a resume command for a session
- **THEN** Claude sessions use only `claude_startup_flags` and Codex sessions use only `codex_startup_flags`

#### Scenario: Backward-compatible missing config
- **WHEN** `~/.config/clip.toml` is missing or omits `codex_startup_flags`
- **THEN** the system searches both supported session sources and resumes Codex sessions without appending extra startup flags

### Requirement: Parse Codex Conversation Content
The system SHALL parse Codex session metadata and user-visible user/assistant messages into the normalized session model used by search, preview, and CLI output.

#### Scenario: Metadata extraction
- **WHEN** a Codex transcript includes `session_meta.payload.id`, `session_meta.payload.cwd`, and timestamp fields
- **THEN** the system uses those values as the Codex session id, directory, and activity date

#### Scenario: Message extraction
- **WHEN** a Codex transcript includes user and assistant message records
- **THEN** the system includes the visible text from those records in the session title, turn count, preview, and searchable content

#### Scenario: Operational payloads ignored
- **WHEN** a Codex transcript includes tool calls, command output, token counts, reasoning records, encrypted content, or other operational events
- **THEN** the system does not treat those payloads as chat messages for the title, preview, turn count, or search body

#### Scenario: Synthetic Codex context blocks ignored
- **WHEN** a Codex transcript includes injected XML blocks such as `<environment_context>`, `<skill>`, or `<turn_aborted>` in message records
- **THEN** the system excludes those synthetic blocks from the session title, preview, turn count, and searchable content
- **AND** the first real user-visible prompt is still eligible to become the session title

### Requirement: Filter Synthetic Transcript Messages
The system SHALL exclude source-generated synthetic transcript messages from normalized sessions before title, preview, turn count, and search handling.

#### Scenario: Claude task notification ignored
- **WHEN** a Claude transcript includes a user-role task notification marked with `origin.kind` of `task-notification` or message content beginning with `<task-notification>`
- **THEN** the system does not include that notification in the session title, preview, turn count, or searchable content
- **AND** real user-visible prompts in the same session remain searchable

### Requirement: Search Codex Sessions
The system SHALL match Codex sessions by title, working directory, transcript path, session id, and parsed conversation body using the same interactive and CLI search entry points as Claude sessions.

#### Scenario: Interactive body search
- **WHEN** the user types a search query that appears only in a Codex conversation body
- **THEN** the interactive table shows the matching Codex session

#### Scenario: CLI body search
- **WHEN** the user runs `clip --search <query>` and the query appears only in a Codex conversation body
- **THEN** the CLI prints the matching Codex session and exits successfully

#### Scenario: No duplicate or colliding matches
- **WHEN** Claude and Codex sessions have the same raw session id or matching text
- **THEN** the system tracks search matches by a source-qualified identity so each matching session is filtered and rendered correctly

#### Scenario: Source labels are not search matches
- **WHEN** a query appears only in displayed source labels or source storage roots such as `Claude`, `Codex`, `.claude`, or `.codex`
- **THEN** the system does not match sessions solely because of their source
- **AND** sessions that match real title, working directory, transcript identity, session id, or conversation content still display their source indicators

### Requirement: Preview Codex Sessions
The system SHALL render Codex user and assistant messages in the preview pane with the same highlighting behavior used for existing search matches.

#### Scenario: Highlighted Codex match
- **WHEN** the user selects a Codex session after searching for text present in the parsed conversation
- **THEN** the preview displays the Codex conversation and highlights the matched search text

#### Scenario: Empty Codex preview fallback
- **WHEN** a Codex session has metadata but no parseable user-visible messages
- **THEN** the system does not crash and either omits the session or renders an empty preview according to the shared session validity rules

### Requirement: Distinguish Session Sources
The system SHALL show whether each session result comes from Claude or Codex in mixed interactive and CLI search results.

#### Scenario: Interactive source indicator
- **WHEN** the interactive table contains both Claude and Codex sessions
- **THEN** each row includes a compact source indicator that distinguishes Claude from Codex

#### Scenario: CLI source indicator
- **WHEN** CLI search prints a Codex match
- **THEN** the output identifies the match as a Codex session near the other session metadata

#### Scenario: CLI metadata alignment
- **WHEN** CLI search prints single-digit or double-digit numbered results
- **THEN** the source, directory, match, and command metadata lines begin under the result title text for that result

#### Scenario: CLI snippet labels are not highlighted as matches
- **WHEN** CLI search prints a match snippet with a display label such as `Claude:`, `Codex:`, `Title:`, or `Path:`
- **THEN** highlighting applies to matched text in the snippet body rather than the display label itself

### Requirement: Resume Codex Sessions
The system SHALL resume selected Codex sessions with the Codex CLI while preserving existing Claude resume behavior.

#### Scenario: Interactive Codex resume
- **WHEN** the user presses Enter on a Codex session with an existing working directory
- **THEN** the system launches `codex resume <session-id>` with any configured Codex startup flags from that working directory

#### Scenario: CLI Codex resume command
- **WHEN** CLI search prints a Codex match with an existing working directory
- **THEN** the output includes a command equivalent to `cd <working-directory> && codex resume <session-id>` with any configured Codex startup flags

#### Scenario: Claude resume remains unchanged
- **WHEN** the user resumes a Claude session after Codex support is added
- **THEN** the system continues to launch `claude --resume <session-id>` with existing Claude startup flag behavior and without Codex startup flags
