package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/blevesearch/bleve/v2"
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
)

var homeDir string

type config struct {
	IgnoreDirectories  []string `toml:"ignore_directories"`
	ClaudeStartupFlags string   `toml:"claude_startup_flags"`
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

type jsonlRecord struct {
	Type      string    `json:"type"`
	Timestamp string    `json:"timestamp"`
	CWD       string    `json:"cwd"`
	Message   *msgField `json:"message"`
}

type msgField struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
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
}

func loadSessions() []session {
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
		if s.title == "" {
			continue
		}
		if s.directory != "" && isIgnored(s.directory) {
			continue
		}
		sessions = append(sessions, s)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].date.After(sessions[j].date)
	})

	return sessions
}

func parseSession(path string) session {
	f, err := os.Open(path)
	if err != nil {
		return session{}
	}
	defer f.Close()

	s := session{
		id:   strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		path: path,
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

		if rec.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, rec.Timestamp); err == nil {
				if t.After(s.date) {
					s.date = t
				}
			}
		}

		if rec.Type != "user" && rec.Type != "assistant" {
			continue
		}
		if rec.Message == nil {
			continue
		}

		text := strings.TrimSpace(extractText(rec.Message.Content))
		if text == "" {
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

func extractText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if m["type"] == "text" {
					if t, ok := m["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
		}
		return strings.Join(parts, " ")
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
	skillPrefix    = "Base directory for this skill: "
	reCommandName  = regexp.MustCompile(`<command-name>/([^<]+)</command-name>`)
	reCommandArgs  = regexp.MustCompile(`<command-args>([^<]*)</command-args>`)
	reCommandMsg   = regexp.MustCompile(`<command-message>[^<]*</command-message>`)
)

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

func formatConversation(msgs []chatMessage, terms []string) string {
	var b strings.Builder
	for _, m := range msgs {
		text := normalizeText(m.content)
		if m.role == "user" {
			b.WriteString(previewYou.Render("You:"))
			b.WriteString("\n")
			b.WriteString(styledHighlight(text, terms, previewUser))
		} else {
			b.WriteString(previewClaude.Render("Claude:"))
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

func openOrCreateIndex() (bleve.Index, error) {
	indexPath := filepath.Join(homeDir, ".cache", "clips", "search.index")

	idx, err := bleve.Open(indexPath)
	if err == bleve.ErrorIndexPathDoesNotExist {
		os.MkdirAll(filepath.Dir(indexPath), 0755)
		mapping := bleve.NewIndexMapping()
		idx, err = bleve.New(indexPath, mapping)
		if err != nil {
			return nil, err
		}
		return idx, nil
	}
	return idx, err
}

func syncIndex(idx bleve.Index, sessions []session) {
	currentIDs := make(map[string]struct{})
	for _, s := range sessions {
		currentIDs[s.id] = struct{}{}
	}

	for _, s := range sessions {
		info, err := os.Stat(s.path)
		if err != nil {
			continue
		}
		modTime := info.ModTime().Format(time.RFC3339)

		doc, err := idx.Document(s.id)
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
			if m.role == "user" {
				content.WriteString("You: ")
			} else {
				content.WriteString("Claude: ")
			}
			content.WriteString(m.content)
			content.WriteString("\n")
		}

		idx.Index(s.id, sessionDoc{
			Title:   s.title,
			Dir:     s.directory,
			Content: content.String(),
			Path:    s.path,
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

	q := bleve.NewQueryStringQuery(query)
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

type indexReadyMsg struct {
	index bleve.Index
}

func initialModel() model {
	sessions := loadSessions()

	columns := []table.Column{
		{Title: "Title", Width: 40},
		{Title: "Directory", Width: 20},
		{Title: "Turns", Width: 5},
		{Title: "Date", Width: 12},
	}

	rows := make([]table.Row, len(sessions))
	for i, s := range sessions {
		rows[i] = table.Row{
			s.title,
			shortenDir(s.directory),
			fmt.Sprintf("%d", s.turns),
			relativeTime(s.date),
		}
	}

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
	si.Placeholder = "Filter sessions..."
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
	helpStyle = lipgloss.NewStyle().Foreground(base00)
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
		if m.search.Value() != "" {
			m.filterRows()
		}
		return m, nil

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
					args := []string{"--resume", s.id}
					if cfg.ClaudeStartupFlags != "" {
						args = append(args, strings.Fields(cfg.ClaudeStartupFlags)...)
					}
					c := exec.Command("claude", args...)
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
			m.filterRows()
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
	m.viewer.SetContent(formatConversation(m.sessions[idx].messages, m.matchedTerms))
	m.viewer.GotoTop()
}

func (m *model) filterRows() {
	query := strings.TrimSpace(m.search.Value())
	if query == "" {
		m.table.SetRows(m.allRows)
		m.filteredIndices = m.allIndices()
		m.matchedIDs = nil
		m.matchedTerms = nil
		m.updateViewer()
		return
	}

	if m.index == nil {
		q := strings.ToLower(query)
		var filtered []table.Row
		var indices []int
		for i, row := range m.allRows {
			if strings.Contains(strings.ToLower(row[0]), q) {
				filtered = append(filtered, row)
				indices = append(indices, i)
			}
		}
		m.table.SetRows(filtered)
		m.filteredIndices = indices
		m.updateViewer()
		return
	}

	matchedIDs, matchedTerms := searchSessions(m.index, query)
	m.matchedIDs = matchedIDs
	m.matchedTerms = matchedTerms

	var filtered []table.Row
	var indices []int
	for i, row := range m.allRows {
		if i < len(m.sessions) {
			if _, ok := matchedIDs[m.sessions[i].id]; ok {
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
	if len(cols) == 4 {
		padding := len(cols) * 2 // Cell style has Padding(0,1) = 1 char each side
		available := tableInner - padding
		// Fixed widths for Turns and Date
		turnsW := 5
		dateW := 12
		dirW := available / 4
		titleW := available - turnsW - dateW - dirW
		cols[0].Width = titleW
		cols[1].Width = dirW
		cols[2].Width = turnsW
		cols[3].Width = dateW
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

	help := helpStyle.Render("  tab: switch pane • ↑/↓: navigate • ctrl+c: quit")

	v := tea.NewView(searchBar + "\n" + panes + "\n" + help)
	v.AltScreen = true
	return v
}

func main() {
	m := initialModel()
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
