package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/custom"
	"github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	blevere "github.com/blevesearch/bleve/v2/analysis/tokenizer/regexp"
	"github.com/blevesearch/bleve/v2/mapping"
	index "github.com/blevesearch/bleve_index_api"
	"github.com/bmatcuk/doublestar/v4"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	focusSearch = iota
	focusTable
	focusViewer

	maxSearchResults = 10000

	// Bump this when the index mapping/analyzer or indexed content cleanup changes.
	indexVersion = "6"

	minSearchChars = 3
	searchDebounce = 150 * time.Millisecond
	dotTickRate    = 200 * time.Millisecond
)

var (
	homeDir string
	version = "dev"
)

type config struct {
	IgnoreDirectories  []string `toml:"ignore_directories"`
	ClaudeStartupFlags string   `toml:"claude_startup_flags"`
	CodexStartupFlags  string   `toml:"codex_startup_flags"`
}

var cfg config

func init() {
	homeDir, _ = os.UserHomeDir()
	cfg = loadConfig()
}

func loadConfig() config {
	var c config
	path := filepath.Join(homeDir, ".config", "clip.toml")
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return c
	}
	for _, dir := range c.IgnoreDirectories {
		if !filepath.IsAbs(dir) {
			fmt.Fprintf(os.Stderr, "clip: ignore_directories paths must be absolute: %q\n", dir)
			os.Exit(1)
		}
	}
	return c
}

func isIgnored(dir string) bool {
	for _, pattern := range cfg.IgnoreDirectories {
		// Check the directory and all its parents so that a pattern
		// like "/tmp/pr-review*" also matches "/tmp/pr-review-123/sub".
		d := dir
		for d != "" && d != "/" {
			if matched, _ := doublestar.Match(pattern, d); matched {
				return true
			}
			d = filepath.Dir(d)
		}
	}
	return false
}

type session struct {
	source    sessionSource
	id        string
	path      string
	title     string
	directory string
	turns     int
	date      time.Time
	messages  []chatMessage
}

type chatMessage struct {
	role    string
	content string
}

type sessionSource string

const (
	sourceClaude sessionSource = "claude"
	sourceCodex  sessionSource = "codex"
)

func (s session) sessionSource() sessionSource {
	if s.source == "" {
		return sourceClaude
	}
	return s.source
}

func (s session) sourceLabel() string {
	switch s.sessionSource() {
	case sourceCodex:
		return "Codex"
	default:
		return "Claude"
	}
}

func (s session) rawID() string {
	return s.id
}

func (s session) searchKey() string {
	return string(s.sessionSource()) + ":" + s.rawID()
}

func searchableSessionPath(s session) string {
	var base string
	switch s.sessionSource() {
	case sourceCodex:
		base = filepath.Join(homeDir, ".codex", "sessions")
	default:
		base = filepath.Join(homeDir, ".claude", "projects")
	}
	rel, err := filepath.Rel(base, s.path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return s.path
	}
	return rel
}

func resumeExecutable(s session) string {
	if s.sessionSource() == sourceCodex {
		return "codex"
	}
	return "claude"
}

func resumeArgs(s session) []string {
	if s.sessionSource() == sourceCodex {
		args := []string{"resume", s.rawID()}
		if cfg.CodexStartupFlags != "" {
			args = append(args, strings.Fields(cfg.CodexStartupFlags)...)
		}
		return args
	}

	args := []string{"--resume", s.rawID()}
	if cfg.ClaudeStartupFlags != "" {
		args = append(args, strings.Fields(cfg.ClaudeStartupFlags)...)
	}
	return args
}

func missingDirectoryMessage(s session) string {
	return fmt.Sprintf(
		"\nThe original directory for this session no longer exists:\n  %s\n\n"+
			"The session transcript is still available at:\n  %s\n\n"+
			"To recover this session, start %s and paste this prompt:\n\n"+
			"  Read the file %s — it's a JSONL transcript of a previous conversation. Summarize what we were working on and continue where we left off.\n",
		s.directory, s.path, resumeExecutable(s), s.path,
	)
}

func sessionTitleFilterContains(s session, lowerQuery string) bool {
	for _, value := range []string{s.title, s.directory} {
		if strings.Contains(strings.ToLower(value), lowerQuery) {
			return true
		}
	}
	return false
}

type jsonlRecord struct {
	Type      string      `json:"type"`
	Timestamp string      `json:"timestamp"`
	CWD       string      `json:"cwd"`
	IsMeta    bool        `json:"isMeta"`
	Message   *msgField   `json:"message"`
	Origin    originField `json:"origin"`
}

type msgField struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type originField struct {
	Kind string `json:"kind"`
}

type codexJSONLRecord struct {
	Type      string       `json:"type"`
	Timestamp string       `json:"timestamp"`
	Payload   codexPayload `json:"payload"`
}

type codexPayload struct {
	ID        string      `json:"id"`
	CWD       string      `json:"cwd"`
	Timestamp string      `json:"timestamp"`
	Type      string      `json:"type"`
	Role      string      `json:"role"`
	Text      string      `json:"text"`
	Message   interface{} `json:"message"`
	Content   interface{} `json:"content"`
}

type model struct {
	search          textinput.Model
	table           table.Model
	viewer          viewport.Model
	sessions        []session
	allRows         []table.Row
	filteredIndices []int
	focus           int
	width           int
	height          int
	ready           bool
	selectedIdx     int
	index           bleve.Index
	matchedIDs      map[string]struct{}
	matchedTerms    []string
	exitMessage     string
	searchID        uint64
	searching       bool
	dotCount        int
}

func loadSessions() []session {
	sessions := append(loadClaudeSessions(), loadCodexSessions()...)
	sessions = filterValidSessions(sessions)

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].date.After(sessions[j].date)
	})

	return sessions
}

func loadClaudeSessions() []session {
	base := filepath.Join(homeDir, ".claude", "projects")
	var files []string
	_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.Contains(path, "/subagent") {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})

	var sessions []session
	for _, f := range files {
		s := parseSession(f)
		sessions = append(sessions, s)
	}
	return sessions
}

func loadCodexSessions() []session {
	base := filepath.Join(homeDir, ".codex", "sessions")
	var files []string
	_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})

	var sessions []session
	for _, f := range files {
		sessions = append(sessions, parseCodexSession(f))
	}
	return sessions
}

func filterValidSessions(sessions []session) []session {
	var kept []session
	for _, s := range sessions {
		if s.title == "" {
			continue
		}
		if s.directory != "" && isIgnored(s.directory) {
			continue
		}
		kept = append(kept, s)
	}
	return kept
}

func parseSession(path string) session {
	f, err := os.Open(path)
	if err != nil {
		return session{}
	}
	defer f.Close()

	s := session{
		source: sourceClaude,
		id:     strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		path:   path,
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 10*1024*1024)

	for scanner.Scan() {
		var rec jsonlRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}

		if rec.CWD != "" && s.directory == "" {
			s.directory = rec.CWD
		}

		s.updateDate(rec.Timestamp)

		if rec.Type != "user" && rec.Type != "assistant" {
			continue
		}
		if rec.IsMeta {
			continue
		}
		if rec.Message == nil {
			continue
		}

		text := strings.TrimSpace(extractText(rec.Message.Content))
		if text == "" {
			continue
		}
		text = stripCommonSyntheticBlocks(text)
		if text == "" {
			continue
		}
		if rec.Origin.Kind == "task-notification" || isClaudeTaskNotification(text) {
			continue
		}

		text = stripSkillContent(text)
		text = normalizeCommands(text)

		if rec.Message.Role == "user" {
			s.turns++
			if s.title == "" {
				s.title = truncate(text, 120)
			}
		}

		s.messages = append(s.messages, chatMessage{
			role:    rec.Message.Role,
			content: text,
		})
	}

	return s
}

func parseCodexSession(path string) session {
	f, err := os.Open(path)
	if err != nil {
		return session{}
	}
	defer f.Close()

	s := session{
		source: sourceCodex,
		id:     strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		path:   path,
	}
	seenMessages := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 10*1024*1024)

	for scanner.Scan() {
		var rec codexJSONLRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}

		s.updateDate(rec.Timestamp)
		s.updateDate(rec.Payload.Timestamp)

		switch rec.Type {
		case "session_meta":
			if rec.Payload.ID != "" {
				s.id = rec.Payload.ID
			}
			if rec.Payload.CWD != "" {
				s.directory = rec.Payload.CWD
			}
		case "turn_context":
			if s.directory == "" && rec.Payload.CWD != "" {
				s.directory = rec.Payload.CWD
			}
		case "response_item":
			if rec.Payload.Type != "message" {
				continue
			}
			s.appendCodexMessage(rec.Payload.Role, extractText(rec.Payload.Content), seenMessages)
		default:
			eventType := rec.Type
			if rec.Type == "event_msg" && rec.Payload.Type != "" {
				eventType = rec.Payload.Type
			}
			switch eventType {
			case "user_message":
				s.appendCodexMessage("user", codexEventText(rec.Payload), seenMessages)
			case "agent_message":
				s.appendCodexMessage("assistant", codexEventText(rec.Payload), seenMessages)
			}
		}
	}

	if s.date.IsZero() {
		if info, err := os.Stat(path); err == nil {
			s.date = info.ModTime()
		}
	}

	return s
}

func (s *session) updateDate(timestamp string) {
	if timestamp == "" {
		return
	}
	t, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return
	}
	if t.After(s.date) {
		s.date = t
	}
}

func (s *session) appendCodexMessage(role, text string, seen map[string]struct{}) {
	if role != "user" && role != "assistant" {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	text = stripCodexSyntheticBlocks(text)
	if text == "" {
		return
	}
	text = stripSkillContent(text)
	text = normalizeCommands(text)

	key := role + "\x00" + text
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}

	if role == "user" {
		s.turns++
		if s.title == "" {
			s.title = truncate(text, 120)
		}
	}

	s.messages = append(s.messages, chatMessage{
		role:    role,
		content: text,
	})
}

func codexEventText(payload codexPayload) string {
	for _, candidate := range []string{
		extractText(payload.Message),
		payload.Text,
		extractText(payload.Content),
	} {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return ""
}

var (
	commonSyntheticBlockPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?s)<local-command-caveat>\s*.*?</local-command-caveat>\s*`),
		regexp.MustCompile(`</?local-command-caveat>\s*`),
	}

	codexSyntheticBlockPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?s)# AGENTS\.md instructions for[^\n]*\s*<INSTRUCTIONS>\s*.*?</INSTRUCTIONS>\s*`),
		regexp.MustCompile(`(?s)<environment_context>\s*.*?</environment_context>\s*`),
		regexp.MustCompile(`(?s)<skill>\s*.*?</skill>\s*`),
		regexp.MustCompile(`(?s)<turn_aborted>\s*.*?</turn_aborted>\s*`),
	}
)

func stripSyntheticBlocks(text string, patterns []*regexp.Regexp) string {
	for _, pattern := range patterns {
		text = pattern.ReplaceAllString(text, "")
	}
	return strings.TrimSpace(text)
}

func stripCommonSyntheticBlocks(text string) string {
	return stripSyntheticBlocks(text, commonSyntheticBlockPatterns)
}

func stripCodexSyntheticBlocks(text string) string {
	text = stripCommonSyntheticBlocks(text)
	return stripSyntheticBlocks(text, codexSyntheticBlockPatterns)
}

func extractText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				switch m["type"] {
				case "text", "input_text", "output_text":
					if t, ok := m["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
		}
		return strings.Join(parts, " ")
	case map[string]interface{}:
		for _, key := range []string{"text", "message"} {
			if t, ok := v[key].(string); ok {
				return t
			}
		}
		if c, ok := v["content"]; ok {
			return extractText(c)
		}
	}
	return ""
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

var (
	skillPrefix   = "Base directory for this skill: "
	reCommandName = regexp.MustCompile(`<command-name>/([^<]+)</command-name>`)
	reCommandArgs = regexp.MustCompile(`<command-args>([^<]*)</command-args>`)
	reCommandMsg  = regexp.MustCompile(`<command-message>[^<]*</command-message>`)
)

func isClaudeTaskNotification(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "<task-notification>")
}

func normalizeCommands(text string) string {
	name := reCommandName.FindStringSubmatch(text)
	if name == nil {
		return text
	}
	cmd := "/" + name[1]
	if args := reCommandArgs.FindStringSubmatch(text); args != nil && strings.TrimSpace(args[1]) != "" {
		cmd += " " + strings.TrimSpace(args[1])
	}
	// Remove all command XML tags
	text = reCommandName.ReplaceAllString(text, "")
	text = reCommandArgs.ReplaceAllString(text, "")
	text = reCommandMsg.ReplaceAllString(text, "")
	text = strings.TrimSpace(text)
	if text == "" {
		return cmd
	}
	return cmd + "\n" + text
}

func stripSkillContent(text string) string {
	if strings.HasPrefix(text, skillPrefix) {
		// Extract the skill path from the first line
		firstNewline := strings.Index(text, "\n")
		if firstNewline < 0 {
			return text
		}
		path := strings.TrimPrefix(text[:firstNewline], skillPrefix)
		return "<skill loaded: " + filepath.Base(path) + ">"
	}
	return text
}

var (
	// Solarized palette
	base03 = lipgloss.Color("#002b36")
	base01 = lipgloss.Color("#586e75")
	base00 = lipgloss.Color("#657b83")
	base1  = lipgloss.Color("#93a1a1")
	base2  = lipgloss.Color("#eee8d5")
	cyan   = lipgloss.Color("#2aa198")
	violet = lipgloss.Color("#6c71c4")
	yellow = lipgloss.Color("#b58900")

	accent         = cyan
	highlightColor = yellow
	multiNewline   = regexp.MustCompile(`\n{3,}`)
	hrRule         = regexp.MustCompile(`\n---\n`)
	previewYou     = lipgloss.NewStyle().Foreground(cyan).Bold(true)
	previewClaude  = lipgloss.NewStyle().Foreground(violet).Bold(true)
	previewUser    = lipgloss.NewStyle().Foreground(base2)
	previewAsst    = lipgloss.NewStyle().Foreground(base1)
	highlightStyle = lipgloss.NewStyle().Background(highlightColor).Foreground(base03)
)

func formatConversation(s session, terms []string) string {
	var b strings.Builder
	for _, m := range s.messages {
		text := normalizeText(m.content)
		if m.role == "user" {
			b.WriteString(previewYou.Render("You:"))
			b.WriteString("\n")
			b.WriteString(styledHighlight(text, terms, previewUser))
		} else {
			b.WriteString(previewClaude.Render(s.sourceLabel() + ":"))
			b.WriteString("\n")
			b.WriteString(styledHighlight(text, terms, previewAsst))
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

func styledHighlight(text string, terms []string, baseStyle lipgloss.Style) string {
	lowerTerms := make([]string, len(terms))
	for i, t := range terms {
		lowerTerms[i] = strings.ToLower(t)
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = styledHighlightLine(line, lowerTerms, baseStyle)
	}
	return strings.Join(lines, "\n")
}

func styledHighlightLine(line string, lowerTerms []string, baseStyle lipgloss.Style) string {
	if line == "" {
		return line
	}
	if len(lowerTerms) == 0 {
		return baseStyle.Render(line)
	}
	lower := strings.ToLower(line)

	type span struct{ start, end int }
	var spans []span
	for _, tl := range lowerTerms {
		offset := 0
		for {
			idx := strings.Index(lower[offset:], tl)
			if idx < 0 {
				break
			}
			s := offset + idx
			spans = append(spans, span{s, s + len(tl)})
			offset = s + 1
		}
	}

	if len(spans) == 0 {
		return baseStyle.Render(line)
	}

	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].end > spans[j].end
		}
		return spans[i].start < spans[j].start
	})
	merged := []span{spans[0]}
	for _, s := range spans[1:] {
		last := &merged[len(merged)-1]
		if s.start <= last.end {
			if s.end > last.end {
				last.end = s.end
			}
		} else {
			merged = append(merged, s)
		}
	}

	var b strings.Builder
	pos := 0
	for _, s := range merged {
		if s.start > pos {
			b.WriteString(baseStyle.Render(line[pos:s.start]))
		}
		b.WriteString(highlightStyle.Render(line[s.start:s.end]))
		pos = s.end
	}
	if pos < len(line) {
		b.WriteString(baseStyle.Render(line[pos:]))
	}
	return b.String()
}

func normalizeText(s string) string {
	s = hrRule.ReplaceAllString(s, "\n")
	s = multiNewline.ReplaceAllString(s, "\n\n")
	s = strings.TrimLeft(s, "\n")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimLeft(line, " \t")
	}
	return strings.Join(lines, "\n")
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(math.Round(d.Minutes()))
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", m)
	case d < 24*time.Hour:
		h := int(math.Round(d.Hours()))
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(math.Round(d.Hours() / 24))
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

func shortenDir(dir string) string {
	if strings.HasPrefix(dir, homeDir) {
		return "~" + dir[len(homeDir):]
	}
	return dir
}

type sessionDoc struct {
	Title   string `json:"title"`
	Dir     string `json:"directory"`
	Content string `json:"content"`
	Path    string `json:"path"`
	ModTime string `json:"mod_time"`
}

func newIndexMapping() *mapping.IndexMappingImpl {
	im := bleve.NewIndexMapping()

	im.AddCustomTokenizer("ws_tokenizer", map[string]interface{}{
		"type":   blevere.Name,
		"regexp": `\S+`,
	})
	im.AddCustomAnalyzer("code_friendly", map[string]interface{}{
		"type":          custom.Name,
		"tokenizer":     "ws_tokenizer",
		"token_filters": []string{lowercase.Name},
	})

	docMapping := mapping.NewDocumentMapping()
	for _, field := range []string{"title", "directory", "content", "path"} {
		fm := mapping.NewTextFieldMapping()
		fm.Analyzer = "code_friendly"
		docMapping.AddFieldMappingsAt(field, fm)
	}
	im.DefaultMapping = docMapping
	return im
}

func createNewIndex(path string) (bleve.Index, error) {
	os.MkdirAll(filepath.Dir(path), 0755)
	return bleve.New(path, newIndexMapping())
}

func openOrCreateIndex() (bleve.Index, error) {
	cacheDir := filepath.Join(homeDir, ".cache", "clips")
	indexPath := filepath.Join(cacheDir, "search.index")
	versionPath := filepath.Join(cacheDir, "search.version")

	if v, err := os.ReadFile(versionPath); err != nil || strings.TrimSpace(string(v)) != indexVersion {
		os.RemoveAll(indexPath)
	}

	idx, err := bleve.Open(indexPath)
	if err != nil {
		os.RemoveAll(indexPath)
		idx, err = createNewIndex(indexPath)
		if err != nil {
			return nil, err
		}
		os.WriteFile(versionPath, []byte(indexVersion), 0644)
	}
	return idx, nil
}

func syncIndex(idx bleve.Index, sessions []session) {
	currentIDs := make(map[string]struct{})
	for _, s := range sessions {
		currentIDs[s.searchKey()] = struct{}{}
	}

	for _, s := range sessions {
		info, err := os.Stat(s.path)
		if err != nil {
			continue
		}
		modTime := info.ModTime().Format(time.RFC3339)

		doc, err := idx.Document(s.searchKey())
		if err == nil && doc != nil {
			var existingModTime string
			doc.VisitFields(func(f index.Field) {
				if f.Name() == "mod_time" {
					existingModTime = string(f.Value())
				}
			})
			if existingModTime == modTime {
				continue
			}
		}

		var content strings.Builder
		for _, m := range s.messages {
			content.WriteString(m.content)
			content.WriteString("\n")
		}

		idx.Index(s.searchKey(), sessionDoc{
			Title:   s.title,
			Dir:     s.directory,
			Content: content.String(),
			Path:    searchableSessionPath(s),
			ModTime: modTime,
		})
	}

	q := bleve.NewMatchAllQuery()
	req := bleve.NewSearchRequest(q)
	req.Size = maxSearchResults
	req.Fields = []string{}
	results, err := idx.Search(req)
	if err != nil {
		return
	}
	for _, hit := range results.Hits {
		if _, ok := currentIDs[hit.ID]; !ok {
			idx.Delete(hit.ID)
		}
	}
}

func searchSessions(idx bleve.Index, query string) (map[string]struct{}, []string) {
	matchedIDs := make(map[string]struct{})
	var matchedTerms []string

	q := bleve.NewWildcardQuery("*" + strings.ToLower(strings.TrimSpace(query)) + "*")
	req := bleve.NewSearchRequest(q)
	req.Size = maxSearchResults
	req.Fields = []string{}
	req.Highlight = bleve.NewHighlight()

	results, err := idx.Search(req)
	if err != nil {
		return matchedIDs, matchedTerms
	}

	termSet := make(map[string]struct{})
	for _, hit := range results.Hits {
		matchedIDs[hit.ID] = struct{}{}
		for _, fragments := range hit.Fragments {
			for _, frag := range fragments {
				parts := strings.Split(frag, "<mark>")
				for i := 1; i < len(parts); i++ {
					if end := strings.Index(parts[i], "</mark>"); end >= 0 {
						term := parts[i][:end]
						termSet[strings.ToLower(term)] = struct{}{}
					}
				}
			}
		}
	}

	for term := range termSet {
		matchedTerms = append(matchedTerms, term)
	}
	return matchedIDs, matchedTerms
}

func searchLoadedSessions(sessions []session, query string) (map[string]struct{}, []string) {
	matchedIDs := make(map[string]struct{})
	q := normalizedSearchQuery(query)
	if q == "" {
		return matchedIDs, nil
	}

	for _, s := range sessions {
		if sessionContains(s, q) {
			matchedIDs[s.searchKey()] = struct{}{}
		}
	}
	if len(matchedIDs) == 0 {
		return matchedIDs, nil
	}
	return matchedIDs, []string{q}
}

func normalizedSearchQuery(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

func sessionContains(s session, lowerQuery string) bool {
	for _, value := range []string{s.rawID(), s.title, s.directory, searchableSessionPath(s)} {
		if strings.Contains(strings.ToLower(value), lowerQuery) {
			return true
		}
	}
	for _, m := range s.messages {
		if strings.Contains(strings.ToLower(m.content), lowerQuery) {
			return true
		}
	}
	return false
}

func mergeSearchResults(
	ids map[string]struct{},
	terms []string,
	extraIDs map[string]struct{},
	extraTerms []string,
) (map[string]struct{}, []string) {
	if ids == nil {
		ids = make(map[string]struct{})
	}
	for id := range extraIDs {
		ids[id] = struct{}{}
	}

	seenTerms := make(map[string]struct{}, len(terms)+len(extraTerms))
	for _, term := range terms {
		seenTerms[strings.ToLower(term)] = struct{}{}
	}
	for _, term := range extraTerms {
		key := strings.ToLower(term)
		if _, ok := seenTerms[key]; ok {
			continue
		}
		terms = append(terms, term)
		seenTerms[key] = struct{}{}
	}
	return ids, terms
}

type indexReadyMsg struct {
	index bleve.Index
}

type searchResultMsg struct {
	id           uint64
	matchedIDs   map[string]struct{}
	matchedTerms []string
	sessions     []session
}

type searchTickMsg struct {
	id uint64
}

type searchDebounceMsg struct {
	id    uint64
	query string
}

var nextSearchID atomic.Uint64

func initialModel() model {
	sessions := loadSessions()

	columns := []table.Column{
		{Title: "Source", Width: 6},
		{Title: "Title", Width: 40},
		{Title: "Directory", Width: 20},
		{Title: "Turns", Width: 5},
		{Title: "Date", Width: 12},
	}

	rows := sessionRows(sessions)

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(7),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		Bold(true).
		Foreground(base1)
	s.Selected = s.Selected.
		Foreground(base03).
		Background(accent).
		Bold(false)
	t.SetStyles(s)

	vp := viewport.New()
	vp.Style = lipgloss.NewStyle()
	vp.SoftWrap = true

	si := textinput.New()
	si.Prompt = " / "
	si.Placeholder = "Filter sessions (titles only — building full-text index...)"
	si.KeyMap.AcceptSuggestion = key.NewBinding(key.WithDisabled())

	allIndices := make([]int, len(sessions))
	for i := range allIndices {
		allIndices[i] = i
	}

	return model{
		search:          si,
		table:           t,
		viewer:          vp,
		sessions:        sessions,
		allRows:         rows,
		filteredIndices: allIndices,
		focus:           focusSearch,
		selectedIdx:     -1,
	}
}

func sessionRows(sessions []session) []table.Row {
	rows := make([]table.Row, len(sessions))
	for i, s := range sessions {
		rows[i] = table.Row{
			s.sourceLabel(),
			s.title,
			shortenDir(s.directory),
			fmt.Sprintf("%d", s.turns),
			relativeTime(s.date),
		}
	}
	return rows
}

var (
	activeBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(accent)
	inactiveBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(base01)
	searchBarStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(accent)
	searchBarInactive = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(base01)
	helpStyle      = lipgloss.NewStyle().Foreground(base00)
	sessionIDStyle = lipgloss.NewStyle().Foreground(base1)
	searchingStyle = lipgloss.NewStyle().Foreground(accent)
)

func (m model) Init() tea.Cmd {
	return func() tea.Msg {
		idx, err := openOrCreateIndex()
		if err != nil {
			return indexReadyMsg{}
		}
		syncIndex(idx, m.sessions)
		return indexReadyMsg{index: idx}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case indexReadyMsg:
		m.index = msg.index
		if m.index != nil {
			m.search.Placeholder = "Search sessions..."
		} else {
			m.search.Placeholder = "Filter sessions (titles only)"
		}
		if m.search.Value() != "" {
			return m, m.startSearch()
		}
		return m, nil

	case searchDebounceMsg:
		if msg.id != m.searchID {
			return m, nil
		}
		return m, m.runAsyncSearch(msg.id, msg.query)

	case searchResultMsg:
		if msg.id != m.searchID {
			return m, nil
		}
		m.sessions = msg.sessions
		m.allRows = sessionRows(m.sessions)
		m.searching = false
		m.matchedIDs = msg.matchedIDs
		m.matchedTerms = msg.matchedTerms
		m.applyFullTextFilter()
		return m, nil

	case searchTickMsg:
		if msg.id != m.searchID || !m.searching {
			return m, nil
		}
		m.dotCount = (m.dotCount + 1) % 4
		return m, tea.Tick(dotTickRate, func(time.Time) tea.Msg {
			return searchTickMsg{id: msg.id}
		})

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		wasReady := m.ready
		m.ready = true
		m.updateSizes()
		if !wasReady {
			return m, m.applyFocus()
		}
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.focus != focusSearch {
				return m, tea.Quit
			}
		case "tab":
			m.focus = (m.focus + 1) % 3
			return m, m.applyFocus()
		case "shift+tab":
			m.focus = (m.focus + 2) % 3
			return m, m.applyFocus()
		case "down":
			if m.focus == focusSearch {
				m.focus = focusTable
				return m, m.applyFocus()
			}
		case "up":
			if m.focus == focusTable && m.table.Cursor() == 0 {
				m.focus = focusSearch
				return m, m.applyFocus()
			}
		case "right":
			if m.focus == focusTable {
				m.focus = focusViewer
				return m, m.applyFocus()
			}
		case "left":
			if m.focus == focusViewer {
				m.focus = focusTable
				return m, m.applyFocus()
			}
		case "enter":
			if m.focus == focusTable || m.focus == focusViewer {
				if s := m.selectedSession(); s != nil {
					if s.directory != "" {
						if _, err := os.Stat(s.directory); os.IsNotExist(err) {
							m.exitMessage = missingDirectoryMessage(*s)
							return m, tea.Quit
						}
					}
					c := exec.Command(resumeExecutable(*s), resumeArgs(*s)...)
					if s.directory != "" {
						c.Dir = s.directory
					}
					return m, tea.ExecProcess(c, func(err error) tea.Msg {
						return tea.QuitMsg{}
					})
				}
			}
		case "/":
			if m.focus != focusSearch {
				m.focus = focusSearch
				return m, m.applyFocus()
			}
		}
	}

	switch m.focus {
	case focusSearch:
		prevValue := m.search.Value()
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		cmds = append(cmds, cmd)
		if m.search.Value() != prevValue {
			cmds = append(cmds, m.startSearch())
		}
	case focusTable:
		prevCursor := m.table.Cursor()
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		cmds = append(cmds, cmd)
		if m.table.Cursor() != prevCursor {
			m.updateViewer()
		}
	case focusViewer:
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) applyFocus() tea.Cmd {
	m.search.Blur()
	m.table.Blur()

	switch m.focus {
	case focusSearch:
		return m.search.Focus()
	case focusTable:
		m.table.Focus()
		m.updateViewer()
	}
	return nil
}

func (m *model) selectedSession() *session {
	if m.selectedIdx >= 0 && m.selectedIdx < len(m.sessions) {
		return &m.sessions[m.selectedIdx]
	}
	return nil
}

func (m *model) updateViewer() {
	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.filteredIndices) {
		m.viewer.SetContent("")
		m.selectedIdx = -1
		return
	}
	idx := m.filteredIndices[cursor]
	m.selectedIdx = idx
	m.viewer.SetContent(formatConversation(m.sessions[idx], m.matchedTerms))
	m.viewer.GotoTop()
}

func (m *model) startSearch() tea.Cmd {
	query := strings.TrimSpace(m.search.Value())

	if query == "" {
		m.table.SetRows(m.allRows)
		m.filteredIndices = m.allIndices()
		m.matchedIDs = nil
		m.matchedTerms = nil
		m.searching = false
		m.updateViewer()
		return nil
	}

	m.applyTitleFilter(query)

	if len(query) < minSearchChars {
		m.matchedIDs = nil
		m.matchedTerms = nil
		m.searching = false
		return nil
	}

	id := nextSearchID.Add(1)
	m.searchID = id
	m.searching = true
	m.dotCount = 0

	return tea.Batch(
		tea.Tick(searchDebounce, func(time.Time) tea.Msg {
			return searchDebounceMsg{id: id, query: query}
		}),
		tea.Tick(dotTickRate, func(time.Time) tea.Msg {
			return searchTickMsg{id: id}
		}),
	)
}

func (m *model) runAsyncSearch(id uint64, query string) tea.Cmd {
	idx := m.index
	return func() tea.Msg {
		sessions := loadSessions()

		matchedIDs := make(map[string]struct{})
		var matchedTerms []string
		if idx != nil {
			matchedIDs, matchedTerms = searchSessions(idx, query)
		}
		loadedIDs, loadedTerms := searchLoadedSessions(sessions, query)
		matchedIDs, matchedTerms = mergeSearchResults(matchedIDs, matchedTerms, loadedIDs, loadedTerms)

		return searchResultMsg{
			id:           id,
			matchedIDs:   matchedIDs,
			matchedTerms: matchedTerms,
			sessions:     sessions,
		}
	}
}

func (m *model) applyTitleFilter(query string) {
	q := strings.ToLower(query)
	var filtered []table.Row
	var indices []int
	for i, row := range m.allRows {
		if i < len(m.sessions) && sessionTitleFilterContains(m.sessions[i], q) {
			filtered = append(filtered, row)
			indices = append(indices, i)
		}
	}
	m.table.SetRows(filtered)
	m.filteredIndices = indices
	m.updateViewer()
}

func (m *model) applyFullTextFilter() {
	var filtered []table.Row
	var indices []int
	for i, row := range m.allRows {
		if i < len(m.sessions) {
			if _, ok := m.matchedIDs[m.sessions[i].searchKey()]; ok {
				filtered = append(filtered, row)
				indices = append(indices, i)
			}
		}
	}
	m.table.SetRows(filtered)
	m.filteredIndices = indices
	m.updateViewer()
}

func (m *model) allIndices() []int {
	indices := make([]int, len(m.sessions))
	for i := range indices {
		indices[i] = i
	}
	return indices
}

func (m *model) updateSizes() {
	if !m.ready {
		return
	}
	searchHeight := 3
	contentHeight := m.height - searchHeight - 1

	halfWidth := m.width / 2

	m.search.SetWidth(m.width - 4)

	// Table: left half
	tableInner := halfWidth - 2
	m.table.SetWidth(tableInner)
	m.table.SetHeight(contentHeight - 2)

	// Distribute column widths: Title gets the lion's share
	cols := m.table.Columns()
	if len(cols) == 5 {
		padding := len(cols) * 2 // Cell style has Padding(0,1) = 1 char each side
		available := tableInner - padding
		// Fixed widths for Turns and Date
		sourceW := 6
		turnsW := 5
		dateW := 12
		dirW := available / 4
		titleW := available - sourceW - turnsW - dateW - dirW
		cols[0].Width = sourceW
		cols[1].Width = titleW
		cols[2].Width = dirW
		cols[3].Width = turnsW
		cols[4].Width = dateW
		m.table.SetColumns(cols)
	}

	// Viewer: right half
	viewerWidth := m.width - halfWidth - 2
	m.viewer.SetWidth(viewerWidth)
	m.viewer.SetHeight(contentHeight - 2)

	m.updateViewer()
}

func (m model) View() tea.View {
	if !m.ready {
		return tea.NewView("Initializing...")
	}

	// Search bar
	var sBorder lipgloss.Style
	if m.focus == focusSearch {
		sBorder = searchBarStyle.Width(m.width)
	} else {
		sBorder = searchBarInactive.Width(m.width)
	}
	searchBar := sBorder.Render(m.search.View())

	// Panes
	var leftBorder, rightBorder lipgloss.Style
	switch m.focus {
	case focusTable:
		leftBorder = activeBorder
		rightBorder = inactiveBorder
	case focusViewer:
		leftBorder = inactiveBorder
		rightBorder = activeBorder
	default:
		leftBorder = inactiveBorder
		rightBorder = inactiveBorder
	}

	left := leftBorder.Render(m.table.View())
	right := rightBorder.Render(m.viewer.View())
	panes := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	helpText := "  tab: switch pane • ↑/↓: navigate • ctrl+c: quit"
	if m.searching {
		dots := strings.Repeat(".", m.dotCount+1)
		helpText += searchingStyle.Render("  searching" + dots)
	}
	status := m.statusBar(helpText)

	v := tea.NewView(searchBar + "\n" + panes + "\n" + status)
	v.AltScreen = true
	return v
}

func (m model) statusBar(helpText string) string {
	left := helpStyle.Render(helpText)
	var right string
	if s := m.selectedSession(); s != nil {
		if id := s.rawID(); id != "" {
			right = truncateLeft("session: "+id+"  ", m.width)
		}
	}
	if right == "" {
		return left
	}
	return renderStatusBar(left, sessionIDStyle.Render(right), m.width)
}

func renderStatusBar(left, right string, width int) string {
	if width <= 0 {
		return left + "  " + right
	}

	rightW := lipgloss.Width(right)
	if rightW >= width {
		return lipgloss.PlaceHorizontal(width, lipgloss.Right, right)
	}

	leftW := lipgloss.Width(left)
	gap := width - leftW - rightW
	if gap < 1 {
		return lipgloss.PlaceHorizontal(width, lipgloss.Right, right)
	}
	return left + strings.Repeat(" ", gap) + right
}

func truncateLeft(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= maxWidth {
		return text
	}

	runes := []rune(text)
	if maxWidth <= 3 {
		return string(runes[len(runes)-maxWidth:])
	}

	suffixWidth := maxWidth - 3
	suffix := make([]rune, 0, suffixWidth)
	width := 0
	for i := len(runes) - 1; i >= 0; i-- {
		w := lipgloss.Width(string(runes[i]))
		if width+w > suffixWidth {
			break
		}
		suffix = append([]rune{runes[i]}, suffix...)
		width += w
	}
	return "..." + string(suffix)
}

func main() {
	var searchQuery string
	var showVersion bool

	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flags.BoolVar(&showVersion, "version", false, "print version")
	flags.BoolVar(&showVersion, "v", false, "print version")
	flags.StringVar(&searchQuery, "s", "", "search sessions and print matches")
	flags.StringVar(&searchQuery, "search", "", "search sessions and print matches")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: %s [-s query]\n", filepath.Base(os.Args[0]))
		flags.PrintDefaults()
	}
	flags.Parse(os.Args[1:])

	if showVersion {
		fmt.Println(version)
		return
	}
	if searchQuery != "" {
		os.Exit(runSearchCLI(searchQuery, os.Stdout))
	}

	m := initialModel()
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
	if fm, ok := final.(model); ok && fm.exitMessage != "" {
		fmt.Print(fm.exitMessage)
	}
}
