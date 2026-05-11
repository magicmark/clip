package main

import (
	"bytes"
	"fmt"
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
		"Source     Claude",
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

func TestSearchCLIDoesNotMatchDisplayedSourceLabel(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := homeDir
	homeDir = tmpDir
	t.Cleanup(func() { homeDir = origHome })

	projectDir := filepath.Join(tmpDir, ".claude", "projects", "-tmp-sourceproject")
	os.MkdirAll(projectDir, 0755)
	writeTestSession(t, projectDir, "session-plain", "/tmp/plain source project", []chatMessage{
		{role: "user", content: "plain request"},
		{role: "assistant", content: "plain response"},
	})

	var out bytes.Buffer
	code := runSearchCLI("claude", &out)
	if code != 1 {
		t.Fatalf("expected source label not to create a match, got code %d and output:\n%s", code, stripANSI(out.String()))
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output for source-label-only query, got:\n%s", stripANSI(out.String()))
	}
}

func TestSearchCLIAlignsMetadataUnderTitleForDoubleDigitResults(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := homeDir
	homeDir = tmpDir
	t.Cleanup(func() { homeDir = origHome })

	projectDir := filepath.Join(tmpDir, ".claude", "projects", "-tmp-alignproject")
	os.MkdirAll(projectDir, 0755)
	for i := 0; i < 10; i++ {
		writeTestSession(t, projectDir, fmt.Sprintf("session-%02d", i), "/tmp/align project", []chatMessage{
			{role: "user", content: fmt.Sprintf("needle title %02d", i)},
			{role: "assistant", content: "needle response"},
		})
	}

	var out bytes.Buffer
	code := runSearchCLI("needle", &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	lines := strings.Split(stripANSI(out.String()), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "10. ") {
			continue
		}
		titleCol := strings.Index(line, "needle title")
		if titleCol < 0 {
			t.Fatalf("could not find title text in line %q", line)
		}
		if i+1 >= len(lines) {
			t.Fatalf("missing metadata line after %q", line)
		}
		sourceCol := strings.Index(lines[i+1], "Source")
		if sourceCol != titleCol {
			t.Fatalf("metadata starts at column %d, want title column %d\nentry:  %q\nsource: %q", sourceCol, titleCol, line, lines[i+1])
		}
		return
	}
	t.Fatalf("did not find a double-digit result in output:\n%s", stripANSI(out.String()))
}

func TestHighlightSnippetDoesNotHighlightDisplayLabel(t *testing.T) {
	got := highlightSnippet("Claude: Claude appears in the message", "claude")
	if !strings.HasPrefix(got, "Claude: ") {
		t.Fatalf("expected display label to stay unstyled at the start, got %q", got)
	}
	if got == "Claude: Claude appears in the message" {
		t.Fatalf("expected message body match to be highlighted")
	}
}

func TestSearchCLIPrintsCodexResumeWithStartupFlags(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := homeDir
	origCfg := cfg
	homeDir = tmpDir
	cfg = config{CodexStartupFlags: "--sandbox workspace-write"}
	t.Cleanup(func() {
		homeDir = origHome
		cfg = origCfg
	})

	codexDir := filepath.Join(tmpDir, ".codex", "sessions", "2026", "05", "10")
	os.MkdirAll(codexDir, 0755)
	writeCodexTestSession(t, codexDir, "codex-live-file", "codex-live", "/tmp/codex project", []chatMessage{
		{role: "user", content: "please find the codex needle"},
		{role: "assistant", content: "I found the codex needle"},
	})

	var out bytes.Buffer
	code := runSearchCLI("needle", &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	got := stripANSI(out.String())
	for _, want := range []string{
		`1 match for "needle"`,
		"1. please find the codex needle",
		"Source     Codex",
		"Directory  /tmp/codex project",
		"Match      Title: please find the codex needle",
		"Command    cd '/tmp/codex project' && codex resume codex-live --sandbox workspace-write",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestResumeCommandCodexWithoutStartupFlags(t *testing.T) {
	origCfg := cfg
	cfg = config{}
	t.Cleanup(func() { cfg = origCfg })

	got := resumeCommand(session{
		source:    sourceCodex,
		id:        "codex-live",
		directory: "/tmp/codex project",
	})
	want := "cd '/tmp/codex project' && codex resume codex-live"
	if got != want {
		t.Fatalf("resumeCommand() = %q, want %q", got, want)
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
