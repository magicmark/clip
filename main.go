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

	"github.com/blevesearch/bleve/v2"
	index "github.com/blevesearch/bleve_index_api"

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
)

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
	search      textinput.Model
	table       table.Model
	viewer      viewport.Model
	sessions    []session
	allRows     []table.Row
	focus       int
	width       int
	height      int
	ready        bool
	selectedIdx  int
	index        bleve.Index
	matchedIDs   map[string]bool
	matchedTerms []string
}

func loadSessions() []session {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".claude", "projects")

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
		if s.title != "" {
			sessions = append(sessions, s)
		}
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
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

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

		text := extractText(rec.Message.Content)
		if text == "" {
			continue
		}

		text = stripSkillContent(text)

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

var skillPrefix = "Base directory for this skill: "

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
	accent         = lipgloss.Color("#2aa198") // solarized cyan
	highlightColor = lipgloss.Color("#b58900") // solarized yellow
	multiNewline   = regexp.MustCompile(`\n{3,}`)
	hrRule         = regexp.MustCompile(`\n---\n`)
	previewYou     = lipgloss.NewStyle().Foreground(lipgloss.Color("#2aa198")).Bold(true) // cyan for "You:"
	previewClaude  = lipgloss.NewStyle().Foreground(lipgloss.Color("#6c71c4")).Bold(true) // violet for "Claude:"
	previewUser    = lipgloss.NewStyle().Foreground(lipgloss.Color("#eee8d5"))            // solarized base2
	previewAsst    = lipgloss.NewStyle().Foreground(lipgloss.Color("#93a1a1"))            // solarized base1
	highlightStyle = lipgloss.NewStyle().Background(highlightColor).Foreground(lipgloss.Color("#002b36")) // base03 on yellow
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
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if len(terms) == 0 {
			if line != "" {
				lines[i] = baseStyle.Render(line)
			}
		} else {
			lines[i] = styledHighlightLine(line, terms, baseStyle)
		}
	}
	return strings.Join(lines, "\n")
}

func styledHighlightLine(line string, terms []string, baseStyle lipgloss.Style) string {
	if line == "" {
		return line
	}
	lower := strings.ToLower(line)

	type span struct{ start, end int }
	var spans []span
	for _, term := range terms {
		tl := strings.ToLower(term)
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
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(dir, home) {
		return "~" + dir[len(home):]
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
	home, _ := os.UserHomeDir()
	indexPath := filepath.Join(home, ".cache", "clips", "search.index")

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
	currentIDs := make(map[string]bool)
	for _, s := range sessions {
		currentIDs[s.id] = true
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
	req.Size = 10000
	req.Fields = []string{}
	results, err := idx.Search(req)
	if err != nil {
		return
	}
	for _, hit := range results.Hits {
		if !currentIDs[hit.ID] {
			idx.Delete(hit.ID)
		}
	}
}

func searchSessions(idx bleve.Index, query string) (map[string]bool, []string) {
	matchedIDs := make(map[string]bool)
	var matchedTerms []string

	q := bleve.NewQueryStringQuery(query)
	req := bleve.NewSearchRequest(q)
	req.Size = 10000
	req.Fields = []string{"title", "content"}
	req.Highlight = bleve.NewHighlight()

	results, err := idx.Search(req)
	if err != nil {
		return matchedIDs, matchedTerms
	}

	termSet := make(map[string]bool)
	for _, hit := range results.Hits {
		matchedIDs[hit.ID] = true
		for _, fragments := range hit.Fragments {
			for _, frag := range fragments {
				parts := strings.Split(frag, "<mark>")
				for i := 1; i < len(parts); i++ {
					if end := strings.Index(parts[i], "</mark>"); end >= 0 {
						term := parts[i][:end]
						termSet[strings.ToLower(term)] = true
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
		Foreground(lipgloss.Color("#93a1a1")) // solarized base1
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#002b36")). // solarized base03
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

	return model{
		search:      si,
		table:       t,
		viewer:      vp,
		sessions:    sessions,
		allRows:     rows,
		focus:       focusSearch,
		selectedIdx: -1,
	}
}

var (
	activeBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(accent)
	inactiveBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#586e75")) // solarized base01
	searchBarStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(accent)
	searchBarInactive = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#586e75")) // solarized base01
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#657b83")) // solarized base00
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
			var initCmds []tea.Cmd
			m.applyFocus(&initCmds)
			return m, tea.Batch(initCmds...)
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
			m.applyFocus(&cmds)
			return m, tea.Batch(cmds...)
		case "shift+tab":
			m.focus = (m.focus + 2) % 3
			m.applyFocus(&cmds)
			return m, tea.Batch(cmds...)
		case "down":
			if m.focus == focusSearch {
				m.focus = focusTable
				m.applyFocus(&cmds)
				return m, tea.Batch(cmds...)
			}
		case "up":
			if m.focus == focusTable && m.table.Cursor() == 0 {
				m.focus = focusSearch
				m.applyFocus(&cmds)
				return m, tea.Batch(cmds...)
			}
		case "right":
			if m.focus == focusTable {
				m.focus = focusViewer
				m.applyFocus(&cmds)
				return m, tea.Batch(cmds...)
			}
		case "left":
			if m.focus == focusViewer {
				m.focus = focusTable
				m.applyFocus(&cmds)
				return m, tea.Batch(cmds...)
			}
		case "enter":
			if m.focus == focusTable || m.focus == focusViewer {
				if s := m.selectedSession(); s != nil {
					c := exec.Command("claude", "--resume", s.id)
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
				m.applyFocus(&cmds)
				return m, tea.Batch(cmds...)
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

func (m *model) applyFocus(cmds *[]tea.Cmd) {
	m.search.Blur()
	m.table.Blur()

	switch m.focus {
	case focusSearch:
		*cmds = append(*cmds, m.search.Focus())
	case focusTable:
		m.table.Focus()
		m.updateViewer()
	case focusViewer:
		// viewport doesn't have focus/blur
	}
}

func (m *model) selectedSession() *session {
	if m.selectedIdx >= 0 && m.selectedIdx < len(m.sessions) {
		return &m.sessions[m.selectedIdx]
	}
	return nil
}

func (m *model) updateViewer() {
	row := m.table.SelectedRow()
	if row == nil {
		m.viewer.SetContent("")
		m.selectedIdx = -1
		return
	}
	title := row[0]
	for i, s := range m.sessions {
		if s.title == title {
			m.selectedIdx = i
			m.viewer.SetContent(formatConversation(s.messages, m.matchedTerms))
			m.viewer.GotoTop()
			return
		}
	}
}

func (m *model) filterRows() {
	query := strings.TrimSpace(m.search.Value())
	if query == "" {
		m.table.SetRows(m.allRows)
		m.matchedIDs = nil
		m.matchedTerms = nil
		m.updateViewer()
		return
	}

	if m.index == nil {
		q := strings.ToLower(query)
		var filtered []table.Row
		for _, row := range m.allRows {
			if strings.Contains(strings.ToLower(row[0]), q) {
				filtered = append(filtered, row)
			}
		}
		m.table.SetRows(filtered)
		return
	}

	matchedIDs, matchedTerms := searchSessions(m.index, query)
	m.matchedIDs = matchedIDs
	m.matchedTerms = matchedTerms

	var filtered []table.Row
	for i, row := range m.allRows {
		if i < len(m.sessions) && matchedIDs[m.sessions[i].id] {
			filtered = append(filtered, row)
		}
	}
	m.table.SetRows(filtered)
	m.updateViewer()
}

func (m *model) updateSizes() {
	if !m.ready {
		return
	}
	searchHeight := 3
	contentHeight := m.height - searchHeight - 1

	halfWidth := m.width / 2

	m.search.SetWidth(m.width - 6)

	// Table: left half
	tableInner := halfWidth - 2
	m.table.SetWidth(tableInner)
	m.table.SetHeight(contentHeight - 2)

	// Distribute column widths: Title gets the lion's share
	cols := m.table.Columns()
	if len(cols) == 4 {
		gap := len(cols) + 1
		available := tableInner - gap
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
		sBorder = searchBarStyle.Width(m.width - 2)
	} else {
		sBorder = searchBarInactive.Width(m.width - 2)
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
