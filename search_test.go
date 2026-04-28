package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

	if _, ok := matchedIDs["session-body-match"]; !ok {
		t.Errorf("expected 'session-body-match' in results when searching for 'quantum' (appears in message body)")
	}
	if _, ok := matchedIDs["session-title-match"]; ok {
		t.Errorf("did not expect 'session-title-match' in results for 'quantum'")
	}
	if _, ok := matchedIDs["session-no-match"]; ok {
		t.Errorf("did not expect 'session-no-match' in results for 'quantum'")
	}
	if len(matchedTerms) == 0 {
		t.Errorf("expected matched terms for 'quantum', got none")
	}

	// Search for "banana" — should match the title-match session
	matchedIDs2, _ := searchSessions(idx, "banana")
	if _, ok := matchedIDs2["session-title-match"]; !ok {
		t.Errorf("expected 'session-title-match' in results when searching for 'banana'")
	}

	// Search for "nonexistent" — should match nothing
	matchedIDs3, _ := searchSessions(idx, "nonexistent")
	if len(matchedIDs3) != 0 {
		t.Errorf("expected no results for 'nonexistent', got %d", len(matchedIDs3))
	}

	// Search for underscore-containing terms (tokenizer must preserve these)
	matchedIDs4, _ := searchSessions(idx, "banana_recipes")
	if _, ok := matchedIDs4["session-title-match"]; ok {
		t.Errorf("did not expect 'session-title-match' for 'banana_recipes' (no underscored term in content)")
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
		_, found := matchedIDs["session-code"]
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
	if _, ok := matchedIDs["session-dir"]; !ok {
		t.Errorf("expected 'session-dir' in results when searching for directory name 'specialproject'")
	}
}
