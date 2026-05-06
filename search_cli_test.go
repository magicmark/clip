package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestSearchCLIPrintsMatchedSessions(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := homeDir
	origCfg := cfg
	homeDir = tmpDir
	cfg = config{ClaudeStartupFlags: "--dangerously-skip-permissions"}
	t.Cleanup(func() {
		homeDir = origHome
		cfg = origCfg
	})

	projectDir := filepath.Join(tmpDir, ".claude", "projects", "-tmp-liveproject")
	os.MkdirAll(projectDir, 0755)
	writeTestSession(t, projectDir, "session-live", "/tmp/live project", []chatMessage{
		{role: "user", content: "please update the ticket"},
		{role: "assistant", content: "I will do that"},
		{role: "user", content: "Stop being so sloppy about the IAM section."},
	})
	writeTestSession(t, projectDir, "session-other", "/tmp/liveproject", []chatMessage{
		{role: "user", content: "hello world"},
		{role: "assistant", content: "hi there"},
	})

	var out bytes.Buffer
	code := runSearchCLI("sloppy", &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	got := stripANSI(out.String())
	for _, want := range []string{
		`1 match for "sloppy"`,
		"1. please update the ticket",
		"Directory  /tmp/live project",
		"Match      You: Stop being so sloppy about the IAM section.",
		"Command    cd '/tmp/live project' && claude --resume session-live --dangerously-skip-permissions",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "session-other") || strings.Contains(got, "hello world") {
		t.Fatalf("unexpected unmatched session in output:\n%s", got)
	}
}

func TestSearchCLINoMatchesReturnsOne(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := homeDir
	homeDir = tmpDir
	t.Cleanup(func() { homeDir = origHome })

	projectDir := filepath.Join(tmpDir, ".claude", "projects", "-tmp-liveproject")
	os.MkdirAll(projectDir, 0755)
	writeTestSession(t, projectDir, "session-live", "/tmp/liveproject", []chatMessage{
		{role: "user", content: "hello world"},
	})

	var out bytes.Buffer
	code := runSearchCLI("sloppy", &out)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output for no matches, got:\n%s", out.String())
	}
}

func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}
