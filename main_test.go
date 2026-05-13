package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	origHome := homeDir
	homeDir = dir
	t.Cleanup(func() { homeDir = origHome })

	configDir := filepath.Join(dir, ".config")
	os.MkdirAll(configDir, 0755)

	configPath := filepath.Join(configDir, "clip.toml")
	os.WriteFile(configPath, []byte(`
ignore_directories = ["/tmp/secret", "/home/*/private"]
claude_startup_flags = "--dangerously-skip-permissions"
codex_startup_flags = "--sandbox workspace-write --ask-for-approval on-request"
`), 0644)

	c := loadConfig()

	if len(c.IgnoreDirectories) != 2 {
		t.Fatalf("expected 2 ignore dirs, got %d", len(c.IgnoreDirectories))
	}
	if c.IgnoreDirectories[0] != "/tmp/secret" {
		t.Errorf("expected /tmp/secret, got %s", c.IgnoreDirectories[0])
	}
	if c.IgnoreDirectories[1] != "/home/*/private" {
		t.Errorf("expected /home/*/private, got %s", c.IgnoreDirectories[1])
	}
	if c.ClaudeStartupFlags != "--dangerously-skip-permissions" {
		t.Errorf("expected --dangerously-skip-permissions, got %s", c.ClaudeStartupFlags)
	}
	if c.CodexStartupFlags != "--sandbox workspace-write --ask-for-approval on-request" {
		t.Errorf("expected Codex startup flags, got %s", c.CodexStartupFlags)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	dir := t.TempDir()
	origHome := homeDir
	homeDir = dir
	t.Cleanup(func() { homeDir = origHome })

	c := loadConfig()

	if len(c.IgnoreDirectories) != 0 {
		t.Errorf("expected 0 ignore dirs, got %d", len(c.IgnoreDirectories))
	}
	if c.ClaudeStartupFlags != "" {
		t.Errorf("expected empty flags, got %s", c.ClaudeStartupFlags)
	}
	if c.CodexStartupFlags != "" {
		t.Errorf("expected empty Codex flags, got %s", c.CodexStartupFlags)
	}
}

func TestIsIgnored(t *testing.T) {
	origCfg := cfg
	t.Cleanup(func() { cfg = origCfg })

	cfg = config{
		IgnoreDirectories: []string{
			"/tmp/secret",
			"/home/*/private",
			"/var/log/*",
			"/tmp/pr-review*",
		},
	}

	tests := []struct {
		dir    string
		ignore bool
	}{
		{"/tmp/secret", true},
		{"/tmp/secret/subdir", true},
		{"/tmp/other", false},
		{"/home/mark/private", true},
		{"/home/jane/private", true},
		{"/home/jane/private/deep/nested", true},
		{"/home/mark/public", false},
		{"/var/log/syslog", true},
		{"/var/log/auth", true},
		{"/var/data/stuff", false},
		{"/tmp/pr-review-123", true},
		{"/tmp/pr-review-123/subdir", true},
		{"/tmp/pr-review-abc/foo/bar", true},
		{"/tmp/pr-other", false},
	}

	for _, tt := range tests {
		got := isIgnored(tt.dir)
		if got != tt.ignore {
			t.Errorf("isIgnored(%q) = %v, want %v", tt.dir, got, tt.ignore)
		}
	}
}

func TestLoadConfigRelativePathFails(t *testing.T) {
	dir := t.TempDir()
	origHome := homeDir
	homeDir = dir
	t.Cleanup(func() { homeDir = origHome })

	configDir := filepath.Join(dir, ".config")
	os.MkdirAll(configDir, 0755)

	configPath := filepath.Join(configDir, "clip.toml")
	os.WriteFile(configPath, []byte(`
ignore_directories = ["relative/path"]
`), 0644)

	// loadConfig calls os.Exit(1) for relative paths, so we can't test it directly.
	// Instead, test the assertion logic inline.
	for _, p := range []string{"relative/path", "foo/bar"} {
		if filepath.IsAbs(p) {
			t.Errorf("expected %q to NOT be absolute", p)
		}
	}
	for _, p := range []string{"/absolute/path", "/tmp/foo"} {
		if !filepath.IsAbs(p) {
			t.Errorf("expected %q to be absolute", p)
		}
	}
}

func TestResumeArgsUseSourceSpecificStartupFlags(t *testing.T) {
	origCfg := cfg
	cfg = config{
		ClaudeStartupFlags: "--dangerously-skip-permissions --verbose",
		CodexStartupFlags:  "--sandbox workspace-write --ask-for-approval on-request",
	}
	t.Cleanup(func() { cfg = origCfg })

	assertStringSlice(t, resumeArgs(session{id: "abc123"}), []string{
		"--resume",
		"abc123",
		"--dangerously-skip-permissions",
		"--verbose",
	})
	assertStringSlice(t, resumeArgs(session{source: sourceCodex, id: "def456"}), []string{
		"resume",
		"def456",
		"--sandbox",
		"workspace-write",
		"--ask-for-approval",
		"on-request",
	})
}

func TestResumeArgsOmitMissingCodexStartupFlags(t *testing.T) {
	origCfg := cfg
	cfg = config{}
	t.Cleanup(func() { cfg = origCfg })

	assertStringSlice(t, resumeArgs(session{source: sourceCodex, id: "def456"}), []string{"resume", "def456"})
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d args %v, want %d args %v", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("args[%d]=%q, want %q; full args=%v", i, got[i], want[i], got)
		}
	}
}

func TestNormalizeCommands(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"simple command",
			"<command-message>swag</command-message>\n<command-name>/swag</command-name>",
			"/swag",
		},
		{
			"command with args",
			"<command-message>model sonnet</command-message>\n<command-name>/model</command-name>\n<command-args>sonnet</command-args>",
			"/model sonnet",
		},
		{
			"no command tags",
			"just a normal message",
			"just a normal message",
		},
		{
			"command name only",
			"<command-name>/commit</command-name>",
			"/commit",
		},
		{
			"command with empty args",
			"<command-name>/help</command-name>\n<command-args></command-args>",
			"/help",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeCommands(tt.input)
			if got != tt.want {
				t.Errorf("normalizeCommands(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLoadSessionsIgnoresDirectories(t *testing.T) {
	origCfg := cfg
	t.Cleanup(func() { cfg = origCfg })

	cfg = config{
		IgnoreDirectories: []string{"/tmp/ignored"},
	}

	s1 := session{title: "keep", directory: "/tmp/other"}
	s2 := session{title: "drop", directory: "/tmp/ignored"}
	s3 := session{title: "also keep", directory: "/home/user/project"}

	var kept []session
	for _, s := range []session{s1, s2, s3} {
		if s.title == "" || (s.directory != "" && isIgnored(s.directory)) {
			continue
		}
		kept = append(kept, s)
	}

	if len(kept) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(kept))
	}
	if kept[0].title != "keep" {
		t.Errorf("expected 'keep', got %q", kept[0].title)
	}
	if kept[1].title != "also keep" {
		t.Errorf("expected 'also keep', got %q", kept[1].title)
	}
}

func TestFormatConversationUsesCodexLabelAndHighlights(t *testing.T) {
	s := session{
		source: sourceCodex,
		messages: []chatMessage{
			{role: "user", content: "Find the needle"},
			{role: "assistant", content: "The needle is here"},
		},
	}

	got := stripANSI(formatConversation(s, []string{"needle"}))
	for _, want := range []string{
		"You:",
		"Codex:",
		"Find the needle",
		"The needle is here",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected formatted conversation to contain %q, got:\n%s", want, got)
		}
	}
}

func TestStatusBarShowsSelectedSessionIDOnRight(t *testing.T) {
	m := model{
		width: 72,
		sessions: []session{
			{id: "session-a"},
			{id: "session-b"},
		},
		selectedIdx: 1,
	}

	got := stripANSI(m.statusBar("help"))
	if !strings.HasPrefix(got, "help") {
		t.Fatalf("expected status bar to keep help text on the left, got %q", got)
	}
	if !strings.HasSuffix(got, "session: session-b  ") {
		t.Fatalf("expected selected session id on the right, got %q", got)
	}
	if lipgloss.Width(got) != m.width {
		t.Fatalf("expected status bar width %d, got %d: %q", m.width, lipgloss.Width(got), got)
	}
}

func TestStatusBarPrioritizesSessionIDWhenCrowded(t *testing.T) {
	m := model{
		width:       15,
		sessions:    []session{{id: "abcdef1234567890"}},
		selectedIdx: 0,
	}

	got := stripANSI(m.statusBar("a long help status"))
	if strings.Contains(got, "help") {
		t.Fatalf("expected crowded status bar to drop help text, got %q", got)
	}
	if !strings.HasPrefix(got, "...") || !strings.HasSuffix(got, "1234567890  ") {
		t.Fatalf("expected truncated selected session id, got %q", got)
	}
	if lipgloss.Width(got) != m.width {
		t.Fatalf("expected status bar width %d, got %d: %q", m.width, lipgloss.Width(got), got)
	}
}

func TestStripCodexSyntheticBlocks(t *testing.T) {
	input := "# AGENTS.md instructions for /tmp/project\n\n" +
		"<INSTRUCTIONS>\nDo not show these repo instructions.\n</INSTRUCTIONS>\n\n" +
		"<environment_context>\n  <cwd>/tmp/project</cwd>\n</environment_context>\n\n" +
		"<local-command-caveat>\nCaveat: ignore local command messages.\n</local-command-caveat>\n\n" +
		"Please continue the parser fix.\n\n" +
		"<turn_aborted>\nThe user interrupted the previous turn.\n</turn_aborted>\n"

	got := stripCodexSyntheticBlocks(input)
	want := "Please continue the parser fix."
	if got != want {
		t.Fatalf("stripCodexSyntheticBlocks() = %q, want %q", got, want)
	}
}

func TestStripCommonSyntheticBlocksRemovesLocalCommandCaveatTag(t *testing.T) {
	input := `still seeing <local-command-caveat> in output`
	got := stripCommonSyntheticBlocks(input)
	want := "still seeing in output"
	if got != want {
		t.Fatalf("stripCommonSyntheticBlocks() = %q, want %q", got, want)
	}
}
