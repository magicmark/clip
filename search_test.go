package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testSearchKey(source sessionSource, id string) string {
	return session{source: source, id: id}.searchKey()
}

// writeTestSession creates a fake .jsonl session file with the given messages.
func writeTestSession(t *testing.T, dir, id, cwd string, messages []chatMessage) string {
	t.Helper()
	path := filepath.Join(dir, id+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	ts := time.Now().Format(time.RFC3339)

	// First record sets the CWD
	enc.Encode(map[string]interface{}{
		"type":      "user",
		"timestamp": ts,
		"cwd":       cwd,
		"message": map[string]interface{}{
			"role":    messages[0].role,
			"content": messages[0].content,
		},
	})

	for _, m := range messages[1:] {
		typ := m.role
		enc.Encode(map[string]interface{}{
			"type":      typ,
			"timestamp": ts,
			"message": map[string]interface{}{
				"role":    m.role,
				"content": m.content,
			},
		})
	}

	return path
}

func writeCodexTestSession(t *testing.T, dir, fileName, id, cwd string, messages []chatMessage) string {
	t.Helper()
	path := filepath.Join(dir, fileName+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	ts := time.Now().Format(time.RFC3339Nano)
	if err := enc.Encode(map[string]interface{}{
		"type":      "session_meta",
		"timestamp": ts,
		"payload": map[string]interface{}{
			"id":        id,
			"cwd":       cwd,
			"timestamp": ts,
		},
	}); err != nil {
		t.Fatal(err)
	}

	for _, m := range messages {
		contentType := "output_text"
		if m.role == "user" {
			contentType = "input_text"
		}
		if err := enc.Encode(map[string]interface{}{
			"type":      "response_item",
			"timestamp": ts,
			"payload": map[string]interface{}{
				"type": "message",
				"role": m.role,
				"content": []map[string]interface{}{
					{"type": contentType, "text": m.content},
				},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	return path
}

func TestParseCodexSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	records := []map[string]interface{}{
		{
			"type":      "session_meta",
			"timestamp": "2026-05-08T12:00:00Z",
			"payload": map[string]interface{}{
				"id":        "codex-session-1",
				"cwd":       "/tmp/codex project",
				"timestamp": "2026-05-08T12:00:00Z",
			},
		},
		{
			"type":      "turn_context",
			"timestamp": "2026-05-08T12:00:30Z",
			"payload": map[string]interface{}{
				"cwd": "/tmp/should-not-replace-metadata",
			},
		},
		{
			"type":      "response_item",
			"timestamp": "2026-05-08T12:00:40Z",
			"payload": map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "input_text", "text": "<environment_context>\n  <cwd>/tmp/codex project</cwd>\n  <shell>zsh</shell>\n</environment_context>"},
				},
			},
		},
		{
			"type":      "response_item",
			"timestamp": "2026-05-08T12:00:42Z",
			"payload": map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "input_text", "text": "# AGENTS.md instructions for /tmp/codex project\n\n<INSTRUCTIONS>\nRepo-only instructions should not appear.\n</INSTRUCTIONS>"},
				},
			},
		},
		{
			"type":      "response_item",
			"timestamp": "2026-05-08T12:00:43Z",
			"payload": map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "input_text", "text": "<local-command-caveat>Caveat: ignore local command messages.</local-command-caveat>"},
				},
			},
		},
		{
			"type":      "response_item",
			"timestamp": "2026-05-08T12:00:45Z",
			"payload": map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "input_text", "text": "<skill>\n<name>openspec-explore</name>\nVerbose loaded skill instructions.\n</skill>"},
				},
			},
		},
		{
			"type":      "response_item",
			"timestamp": "2026-05-08T12:00:50Z",
			"payload": map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "input_text", "text": "<turn_aborted>\nThe user interrupted the previous turn.\n</turn_aborted>"},
				},
			},
		},
		{
			"type":      "response_item",
			"timestamp": "2026-05-08T12:01:00Z",
			"payload": map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "input_text", "text": "Find the frobnicator"},
				},
			},
		},
		{
			"type":      "event_msg",
			"timestamp": "2026-05-08T12:01:10Z",
			"payload": map[string]interface{}{
				"type":    "user_message",
				"message": "Find the frobnicator",
			},
		},
		{
			"type":      "response_item",
			"timestamp": "2026-05-08T12:02:00Z",
			"payload": map[string]interface{}{
				"type": "message",
				"role": "assistant",
				"content": []map[string]interface{}{
					{"type": "output_text", "text": "The answer includes a flux capacitor"},
				},
			},
		},
		{
			"type":      "event_msg",
			"timestamp": "2026-05-08T12:02:10Z",
			"payload": map[string]interface{}{
				"type":    "agent_message",
				"message": "The answer includes a flux capacitor",
			},
		},
		{
			"type":      "response_item",
			"timestamp": "2026-05-08T12:03:00Z",
			"payload": map[string]interface{}{
				"type": "function_call",
				"name": "shell",
				"content": []map[string]interface{}{
					{"type": "output_text", "text": "secret tool output"},
				},
			},
		},
		{
			"type":      "event_msg",
			"timestamp": "2026-05-08T12:04:00Z",
			"payload": map[string]interface{}{
				"type":    "token_count",
				"message": "secret operational event",
			},
		},
	}
	for _, record := range records {
		if err := enc.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	s := parseCodexSession(path)
	if s.sessionSource() != sourceCodex {
		t.Fatalf("expected Codex source, got %q", s.sessionSource())
	}
	if s.rawID() != "codex-session-1" {
		t.Fatalf("expected metadata id, got %q", s.rawID())
	}
	if s.directory != "/tmp/codex project" {
		t.Fatalf("expected metadata cwd, got %q", s.directory)
	}
	if s.title != "Find the frobnicator" {
		t.Fatalf("expected title from first user message, got %q", s.title)
	}
	if s.turns != 1 {
		t.Fatalf("expected duplicate event user message not to increment turns, got %d", s.turns)
	}
	if len(s.messages) != 2 {
		t.Fatalf("expected 2 visible messages, got %#v", s.messages)
	}
	gotContent := s.messages[0].content + "\n" + s.messages[1].content
	for _, unwanted := range []string{
		"<environment_context>",
		"# AGENTS.md instructions",
		"Repo-only instructions",
		"<local-command-caveat>",
		"<skill>",
		"<turn_aborted>",
		"secret tool output",
		"secret operational event",
	} {
		if strings.Contains(gotContent, unwanted) {
			t.Fatalf("unexpected operational content %q in parsed messages: %#v", unwanted, s.messages)
		}
	}
	wantDate, _ := time.Parse(time.RFC3339, "2026-05-08T12:04:00Z")
	if !s.date.Equal(wantDate) {
		t.Fatalf("expected latest record timestamp %s, got %s", wantDate, s.date)
	}
}

func TestParseCodexSessionUsesTurnContextCWDFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, record := range []map[string]interface{}{
		{
			"type": "session_meta",
			"payload": map[string]interface{}{
				"id": "codex-session-2",
			},
		},
		{
			"type": "turn_context",
			"payload": map[string]interface{}{
				"cwd": "/tmp/fallback-cwd",
			},
		},
		{
			"type": "response_item",
			"payload": map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "input_text", "text": "Use fallback cwd"},
				},
			},
		},
	} {
		if err := enc.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	s := parseCodexSession(path)
	if s.directory != "/tmp/fallback-cwd" {
		t.Fatalf("expected turn_context cwd fallback, got %q", s.directory)
	}
}

func TestParseClaudeSessionSkipsTaskNotifications(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude-session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, record := range []map[string]interface{}{
		{
			"type":      "user",
			"timestamp": "2026-05-08T15:44:18Z",
			"cwd":       "/tmp/claude-project",
			"origin": map[string]interface{}{
				"kind": "task-notification",
			},
			"message": map[string]interface{}{
				"role": "user",
				"content": "<task-notification>\n" +
					"<task-id>a9bba9c59c545eb1d</task-id>\n" +
					"<summary>Agent \"Fetch Yelp PR details\" completed</summary>\n" +
					"</task-notification>\n" +
					"Full transcript available at: /tmp/task.output",
			},
		},
		{
			"type":      "user",
			"timestamp": "2026-05-08T15:44:30Z",
			"isMeta":    true,
			"message": map[string]interface{}{
				"role":    "user",
				"content": "<local-command-caveat>Caveat: ignore local command messages.</local-command-caveat>",
			},
		},
		{
			"type":      "user",
			"timestamp": "2026-05-08T15:44:40Z",
			"message": map[string]interface{}{
				"role":    "user",
				"content": "<local-command-caveat>Caveat: ignore local command messages.</local-command-caveat>",
			},
		},
		{
			"type":      "user",
			"timestamp": "2026-05-08T15:45:00Z",
			"message": map[string]interface{}{
				"role":    "user",
				"content": "Where did I comment about payload detection?",
			},
		},
		{
			"type":      "assistant",
			"timestamp": "2026-05-08T15:45:10Z",
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": "I found the relevant comments.",
			},
		},
	} {
		if err := enc.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	s := parseSession(path)
	if s.title != "Where did I comment about payload detection?" {
		t.Fatalf("expected real user prompt as title, got %q", s.title)
	}
	if s.turns != 1 {
		t.Fatalf("expected task notification not to count as a user turn, got %d", s.turns)
	}
	if len(s.messages) != 2 {
		t.Fatalf("expected 2 visible messages, got %#v", s.messages)
	}
	for _, m := range s.messages {
		if strings.Contains(m.content, "<task-notification>") ||
			strings.Contains(m.content, "a9bba9c59c545eb1d") ||
			strings.Contains(m.content, "Fetch Yelp PR details") ||
			strings.Contains(m.content, "local-command-caveat") ||
			strings.Contains(m.content, "ignore local command") {
			t.Fatalf("unexpected task notification in parsed message: %#v", m)
		}
	}

	matchedIDs, _ := searchLoadedSessions([]session{s}, "a9bba9c59c545eb1d")
	if len(matchedIDs) != 0 {
		t.Fatalf("expected task notification id not to be searchable, got %v", matchedIDs)
	}
	matchedIDs, _ = searchLoadedSessions([]session{s}, "local-command-caveat")
	if len(matchedIDs) != 0 {
		t.Fatalf("expected local command caveat not to be searchable, got %v", matchedIDs)
	}
}

func TestSearchMatchesMessageBodies(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, ".claude", "projects", "-tmp-myproject")
	os.MkdirAll(projectDir, 0755)

	// Session where "quantum" only appears in assistant body, not in title
	writeTestSession(t, projectDir, "session-body-match", "/tmp/myproject", []chatMessage{
		{role: "user", content: "tell me about physics"},
		{role: "assistant", content: "Let me explain quantum mechanics to you"},
		{role: "user", content: "thanks that was helpful"},
	})

	// Session where "banana" only appears in title
	writeTestSession(t, projectDir, "session-title-match", "/tmp/myproject", []chatMessage{
		{role: "user", content: "I want to talk about banana recipes"},
		{role: "assistant", content: "Sure, here are some fruit recipes"},
	})

	// Session with no mention of either term
	writeTestSession(t, projectDir, "session-no-match", "/tmp/myproject", []chatMessage{
		{role: "user", content: "hello world"},
		{role: "assistant", content: "hi there"},
	})

	// Parse all sessions
	var sessions []session
	files, _ := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
	for _, f := range files {
		s := parseSession(f)
		if s.title != "" {
			sessions = append(sessions, s)
		}
	}

	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}

	// Build an index in a temp directory
	indexPath := filepath.Join(tmpDir, "test.index")
	idx, err := createNewIndex(indexPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	syncIndex(idx, sessions)

	// Search for "quantum" — should match the body-match session only
	matchedIDs, matchedTerms := searchSessions(idx, "quantum")

	if _, ok := matchedIDs[testSearchKey(sourceClaude, "session-body-match")]; !ok {
		t.Errorf("expected 'session-body-match' in results when searching for 'quantum' (appears in message body)")
	}
	if _, ok := matchedIDs[testSearchKey(sourceClaude, "session-title-match")]; ok {
		t.Errorf("did not expect 'session-title-match' in results for 'quantum'")
	}
	if _, ok := matchedIDs[testSearchKey(sourceClaude, "session-no-match")]; ok {
		t.Errorf("did not expect 'session-no-match' in results for 'quantum'")
	}
	if len(matchedTerms) == 0 {
		t.Errorf("expected matched terms for 'quantum', got none")
	}

	// Search for "banana" — should match the title-match session
	matchedIDs2, _ := searchSessions(idx, "banana")
	if _, ok := matchedIDs2[testSearchKey(sourceClaude, "session-title-match")]; !ok {
		t.Errorf("expected 'session-title-match' in results when searching for 'banana'")
	}

	// Search for "nonexistent" — should match nothing
	matchedIDs3, _ := searchSessions(idx, "nonexistent")
	if len(matchedIDs3) != 0 {
		t.Errorf("expected no results for 'nonexistent', got %d", len(matchedIDs3))
	}

	// Search for underscore-containing terms (tokenizer must preserve these)
	matchedIDs4, _ := searchSessions(idx, "banana_recipes")
	if _, ok := matchedIDs4[testSearchKey(sourceClaude, "session-title-match")]; ok {
		t.Errorf("did not expect 'session-title-match' for 'banana_recipes' (no underscored term in content)")
	}
}

func TestSearchLoadedSessionsMatchesUnindexedBody(t *testing.T) {
	sessions := []session{
		{
			id:    "session-live",
			title: "ticket cleanup",
			messages: []chatMessage{
				{role: "user", content: "Stop being so sloppy."},
			},
		},
		{
			id:    "session-other",
			title: "unrelated",
			messages: []chatMessage{
				{role: "user", content: "hello world"},
			},
		},
	}

	matchedIDs, matchedTerms := searchLoadedSessions(sessions, "sloppy")
	if _, ok := matchedIDs[testSearchKey(sourceClaude, "session-live")]; !ok {
		t.Fatalf("expected session-live to match loaded message body")
	}
	if _, ok := matchedIDs[testSearchKey(sourceClaude, "session-other")]; ok {
		t.Fatalf("did not expect session-other to match")
	}
	if len(matchedTerms) != 1 || matchedTerms[0] != "sloppy" {
		t.Fatalf("expected sloppy matched term, got %v", matchedTerms)
	}
}

func TestAsyncSearchReloadsSessionsFromDisk(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := homeDir
	homeDir = tmpDir
	t.Cleanup(func() { homeDir = origHome })

	projectDir := filepath.Join(tmpDir, ".claude", "projects", "-tmp-liveproject")
	os.MkdirAll(projectDir, 0755)
	path := writeTestSession(t, projectDir, "session-live", "/tmp/liveproject", []chatMessage{
		{role: "user", content: "initial request"},
		{role: "assistant", content: "initial response"},
	})

	sessionsBefore := loadSessions()
	if len(sessionsBefore) != 1 {
		t.Fatalf("expected 1 session before append, got %d", len(sessionsBefore))
	}
	if sessionContains(sessionsBefore[0], "sloppy") {
		t.Fatalf("test setup unexpectedly contains sloppy before append")
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	err = enc.Encode(map[string]interface{}{
		"type":      "user",
		"timestamp": time.Now().Format(time.RFC3339),
		"message": map[string]interface{}{
			"role":    "user",
			"content": "Stop being so sloppy.",
		},
	})
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}

	m := model{sessions: sessionsBefore}
	msg := m.runAsyncSearch(7, "sloppy")().(searchResultMsg)

	if msg.id != 7 {
		t.Fatalf("expected search id 7, got %d", msg.id)
	}
	if _, ok := msg.matchedIDs[testSearchKey(sourceClaude, "session-live")]; !ok {
		t.Fatalf("expected reloaded session to match appended body text")
	}
	if len(msg.sessions) != 1 || msg.sessions[0].turns != 2 {
		t.Fatalf("expected reloaded session with 2 turns, got %#v", msg.sessions)
	}
}

func TestSearchMatchesUnderscoreTerms(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, ".claude", "projects", "-tmp-codeproject")
	os.MkdirAll(projectDir, 0755)

	writeTestSession(t, projectDir, "session-code", "/tmp/codeproject", []chatMessage{
		{role: "user", content: "can you look at main_test.go"},
		{role: "assistant", content: "I see search_test.go and main_test.go in the directory"},
	})

	writeTestSession(t, projectDir, "session-other", "/tmp/codeproject", []chatMessage{
		{role: "user", content: "hello"},
		{role: "assistant", content: "hi there"},
	})

	var sessions []session
	files, _ := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
	for _, f := range files {
		s := parseSession(f)
		if s.title != "" {
			sessions = append(sessions, s)
		}
	}

	indexPath := filepath.Join(tmpDir, "test.index")
	idx, err := createNewIndex(indexPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	syncIndex(idx, sessions)

	tests := []struct {
		query   string
		wantHit bool
	}{
		{"main_test", true},
		{"main_test.go", true},
		{"search_test", true},
		{"search_test.go", true},
		{"main", true},
		{"nonexistent_func", false},
	}

	for _, tt := range tests {
		matchedIDs, _ := searchSessions(idx, tt.query)
		_, found := matchedIDs[testSearchKey(sourceClaude, "session-code")]
		if found != tt.wantHit {
			t.Errorf("search(%q): got hit=%v, want hit=%v", tt.query, found, tt.wantHit)
		}
	}
}

func TestSearchMatchesDirectoryPath(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, ".claude", "projects", "-tmp-specialproject")
	os.MkdirAll(projectDir, 0755)

	writeTestSession(t, projectDir, "session-dir", "/tmp/specialproject", []chatMessage{
		{role: "user", content: "generic title here"},
		{role: "assistant", content: "generic response"},
	})

	var sessions []session
	files, _ := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
	for _, f := range files {
		s := parseSession(f)
		if s.title != "" {
			sessions = append(sessions, s)
		}
	}

	indexPath := filepath.Join(tmpDir, "test.index")
	idx, err := createNewIndex(indexPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	syncIndex(idx, sessions)

	matchedIDs, _ := searchSessions(idx, "specialproject")
	if _, ok := matchedIDs[testSearchKey(sourceClaude, "session-dir")]; !ok {
		t.Errorf("expected 'session-dir' in results when searching for directory name 'specialproject'")
	}
}

func TestMixedSourceSearchUsesQualifiedKeysAndSharedIgnores(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := homeDir
	origCfg := cfg
	homeDir = tmpDir
	cfg = config{IgnoreDirectories: []string{"/tmp/ignored-codex"}}
	t.Cleanup(func() {
		homeDir = origHome
		cfg = origCfg
	})

	claudeDir := filepath.Join(tmpDir, ".claude", "projects", "-tmp-mixed")
	codexDir := filepath.Join(tmpDir, ".codex", "sessions", "2026", "05", "10")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}

	writeTestSession(t, claudeDir, "same-id", "/tmp/claude-project", []chatMessage{
		{role: "user", content: "Claude asks about shared-token"},
		{role: "assistant", content: "Claude response"},
	})
	writeCodexTestSession(t, codexDir, "codex-same-id", "same-id", "/tmp/codex-project", []chatMessage{
		{role: "user", content: "Codex asks about shared-token"},
		{role: "assistant", content: "Codex response"},
	})
	writeCodexTestSession(t, codexDir, "codex-ignored", "ignored-id", "/tmp/ignored-codex", []chatMessage{
		{role: "user", content: "ignored shared-token"},
	})

	sessions := loadSessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 kept sessions, got %#v", sessions)
	}

	loadedIDs, _ := searchLoadedSessions(sessions, "shared-token")
	for _, key := range []string{
		testSearchKey(sourceClaude, "same-id"),
		testSearchKey(sourceCodex, "same-id"),
	} {
		if _, ok := loadedIDs[key]; !ok {
			t.Fatalf("expected loaded search to include %q, got %v", key, loadedIDs)
		}
	}
	if _, ok := loadedIDs[testSearchKey(sourceCodex, "ignored-id")]; ok {
		t.Fatalf("did not expect ignored Codex session in loaded search results")
	}

	indexPath := filepath.Join(tmpDir, "test.index")
	idx, err := createNewIndex(indexPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()
	syncIndex(idx, sessions)

	indexIDs, _ := searchSessions(idx, "shared-token")
	for _, key := range []string{
		testSearchKey(sourceClaude, "same-id"),
		testSearchKey(sourceCodex, "same-id"),
	} {
		if _, ok := indexIDs[key]; !ok {
			t.Fatalf("expected indexed search to include %q, got %v", key, indexIDs)
		}
	}
}

func TestSearchDoesNotMatchDisplayedSourceOrStoragePathLabels(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := homeDir
	origCfg := cfg
	homeDir = tmpDir
	cfg = config{}
	t.Cleanup(func() {
		homeDir = origHome
		cfg = origCfg
	})

	claudeDir := filepath.Join(tmpDir, ".claude", "projects", "-tmp-sourceproject")
	codexDir := filepath.Join(tmpDir, ".codex", "sessions", "2026", "05", "10")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}

	writeTestSession(t, claudeDir, "cl-session", "/tmp/plain source project", []chatMessage{
		{role: "user", content: "plain request"},
		{role: "assistant", content: "plain response"},
	})
	writeCodexTestSession(t, codexDir, "cx-session-file", "cx-session", "/tmp/plain source project", []chatMessage{
		{role: "user", content: "plain request"},
		{role: "assistant", content: "plain response"},
	})

	sessions := loadSessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %#v", sessions)
	}
	for _, query := range []string{"claude", "codex"} {
		loadedIDs, _ := searchLoadedSessions(sessions, query)
		if len(loadedIDs) != 0 {
			t.Fatalf("expected loaded search for %q not to match source metadata, got %v", query, loadedIDs)
		}
	}

	indexPath := filepath.Join(tmpDir, "test.index")
	idx, err := createNewIndex(indexPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()
	syncIndex(idx, sessions)

	for _, query := range []string{"claude", "codex"} {
		indexIDs, _ := searchSessions(idx, query)
		if len(indexIDs) != 0 {
			t.Fatalf("expected indexed search for %q not to match source metadata, got %v", query, indexIDs)
		}
	}
}
