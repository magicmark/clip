package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

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
	ready       bool
	selectedIdx int
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

	s := session{path: path}
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

var multiNewline = regexp.MustCompile(`\n{3,}`)

func formatConversation(msgs []chatMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.role == "user" {
			b.WriteString(">>> USER:\n")
		} else {
			b.WriteString("<<< ASSISTANT:\n")
		}
		text := normalizeText(m.content)
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	return b.String()
}

func normalizeText(s string) string {
	// Collapse 3+ newlines into 2
	s = multiNewline.ReplaceAllString(s, "\n\n")
	// Remove leading whitespace from each line
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
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	vp := viewport.New()
	vp.Style = lipgloss.NewStyle()

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
			BorderForeground(lipgloss.Color("62"))
	inactiveBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))
	searchBarStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))
	searchBarInactive = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240"))
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
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

func (m *model) updateViewer() {
	row := m.table.SelectedRow()
	if row == nil {
		m.viewer.SetContent("")
		m.selectedIdx = -1
		return
	}
	// Find the session that matches this row's title
	title := row[0]
	for i, s := range m.sessions {
		if s.title == title {
			if m.selectedIdx != i {
				m.selectedIdx = i
				m.viewer.SetContent(formatConversation(s.messages))
				m.viewer.GotoTop()
			}
			return
		}
	}
}

func (m *model) filterRows() {
	query := strings.ToLower(strings.TrimSpace(m.search.Value()))
	if query == "" {
		m.table.SetRows(m.allRows)
		return
	}
	var filtered []table.Row
	for _, row := range m.allRows {
		if strings.Contains(strings.ToLower(row[0]), query) {
			filtered = append(filtered, row)
		}
	}
	m.table.SetRows(filtered)
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
