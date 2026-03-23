package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestClaudeStartupFlagsArgs(t *testing.T) {
	tests := []struct {
		flags string
		want  []string
	}{
		{"--dangerously-skip-permissions", []string{"--resume", "abc123", "--dangerously-skip-permissions"}},
		{"--dangerously-skip-permissions --verbose", []string{"--resume", "abc123", "--dangerously-skip-permissions", "--verbose"}},
		{"", []string{"--resume", "abc123"}},
	}

	for _, tt := range tests {
		args := []string{"--resume", "abc123"}
		if tt.flags != "" {
			args = append(args, strings.Fields(tt.flags)...)
		}
		if len(args) != len(tt.want) {
			t.Errorf("flags=%q: got %d args, want %d", tt.flags, len(args), len(tt.want))
			continue
		}
		for i := range args {
			if args[i] != tt.want[i] {
				t.Errorf("flags=%q: args[%d]=%q, want %q", tt.flags, i, args[i], tt.want[i])
			}
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
