# Bleve Full-Text Search Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace title-only substring filtering with full-text search across entire conversation threads using Bleve, with yellow background highlighting of matched terms in the conversation preview.

**Architecture:** Bleve index stored at `~/.cache/clips/search.index`, built incrementally on startup by comparing file mod times. Search executes on every keystroke (same as current behavior). Matched terms extracted from Bleve results and highlighted in the preview pane with yellow background via Lip Gloss.

**Tech Stack:** Go, Bleve v2, Bubble Tea, Lip Gloss v2

---

### Task 1: Add Bleve Dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add Bleve to go.mod**

Run:
```bash
cd /Users/markl/apps/clips2 && go get github.com/blevesearch/bleve/v2
```

**Step 2: Verify it resolves**

Run: `go mod tidy`
Expected: Clean exit, `go.mod` now lists `github.com/blevesearch/bleve/v2`

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "Add Bleve full-text search dependency"
```

---

### Task 2: Add Bleve Index Management Functions

**Files:**
- Modify: `main.go:1-22` (imports)
- Modify: `main.go:57-68` (model struct)
- Add new functions after `shortenDir()` (~line 261)

**Step 1: Add imports and model fields**

Add to imports:
```go
"os"

"github.com/blevesearch/bleve/v2"
```

Add fields to the `model` struct (line 57-68):
```go
type model struct {
	search       textinput.Model
	table        table.Model
	viewer       viewport.Model
	sessions     []session
	allRows      []table.Row
	focus        int
	width        int
	height       int
	ready        bool
	selectedIdx  int
	index        bleve.Index
	matchedIDs   map[string]bool
	matchedTerms []string
}
```

**Step 2: Add sessionDoc struct and indexing functions**

Add after `shortenDir()` (after line 261):

```go
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
	// Build a set of current session IDs
	currentIDs := make(map[string]bool)
	for _, s := range sessions {
		currentIDs[s.id] = true
	}

	// Check each session: index if new or modified
	for _, s := range sessions {
		info, err := os.Stat(s.path)
		if err != nil {
			continue
		}
		modTime := info.ModTime().Format(time.RFC3339)

		// Check if already indexed with same mod time
		doc, err := idx.Document(s.id)
		if err == nil && doc != nil {
			// Visit fields to find mod_time
			var existingModTime string
			doc.VisitFields(func(f bleve.FieldAccessor) {
				if f.Name() == "mod_time" {
					existingModTime = string(f.Value())
				}
			})
			if existingModTime == modTime {
				continue
			}
		}

		// Build content from all messages
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

	// Remove sessions that no longer exist on disk
	// We need to iterate all indexed docs - use a match-all search
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
		// Extract highlighted terms from fragments
		for _, fragments := range hit.Fragments {
			for _, frag := range fragments {
				// Bleve wraps matches in <mark>...</mark>
				// Extract the terms between <mark> tags
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
```

**Step 3: Verify it compiles**

Run: `cd /Users/markl/apps/clips2 && go build -o /dev/null .`
Expected: Clean compile (unused fields warning is fine since we'll wire them in the next task)

**Step 4: Commit**

```bash
git add main.go
git commit -m "Add Bleve index management functions"
```

---

### Task 3: Wire Bleve Into Startup and Search

**Files:**
- Modify: `main.go:263-318` (`initialModel()`)
- Modify: `main.go:485-498` (`filterRows()`)

**Step 1: Update initialModel() to open index and sync**

Replace `initialModel()` (lines 263-318) — add index open + sync at the start:

```go
func initialModel() model {
	sessions := loadSessions()

	idx, err := openOrCreateIndex()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: search index unavailable: %v\n", err)
	}
	if idx != nil {
		syncIndex(idx, sessions)
	}

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
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252"))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("22")).
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
		index:       idx,
	}
}
```

**Step 2: Replace filterRows() with Bleve search**

Replace `filterRows()` (lines 485-498):

```go
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
		// Fallback: title-only substring match if index unavailable
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
```

**Step 3: Verify it compiles and runs**

Run: `cd /Users/markl/apps/clips2 && go build -o /dev/null .`
Expected: Clean compile

**Step 4: Commit**

```bash
git add main.go
git commit -m "Wire Bleve search into startup and filtering"
```

---

### Task 4: Add Highlight Rendering in Conversation Preview

**Files:**
- Modify: `main.go:192-216` (`formatConversation` and styles)
- Modify: `main.go:464-483` (`updateViewer`)

**Step 1: Add highlight style**

Add to the `var` block at line 192:

```go
var (
	multiNewline   = regexp.MustCompile(`\n{3,}`)
	hrRule         = regexp.MustCompile(`\n---\n`)
	previewRole    = lipgloss.NewStyle().Foreground(lipgloss.Color("109")).Bold(true)
	previewUser    = lipgloss.NewStyle().Foreground(lipgloss.Color("223"))
	previewAsst    = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	highlightStyle = lipgloss.NewStyle().Background(lipgloss.Color("178")).Foreground(lipgloss.Color("0"))
)
```

**Step 2: Add highlightTerms function**

Add after `normalizeText()`:

```go
func highlightTerms(text string, terms []string) string {
	if len(terms) == 0 {
		return text
	}
	// Process line by line to preserve newlines (lipgloss styling resets at newlines)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = highlightLine(line, terms)
	}
	return strings.Join(lines, "\n")
}

func highlightLine(line string, terms []string) string {
	if line == "" {
		return line
	}
	lower := strings.ToLower(line)

	// Find all match positions
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
		return line
	}

	// Sort and merge overlapping spans
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

	// Build result with highlights
	var b strings.Builder
	pos := 0
	for _, s := range merged {
		if s.start > pos {
			b.WriteString(line[pos:s.start])
		}
		b.WriteString(highlightStyle.Render(line[s.start:s.end]))
		pos = s.end
	}
	if pos < len(line) {
		b.WriteString(line[pos:])
	}
	return b.String()
}
```

**Step 3: Update formatConversation to accept and apply terms**

Change `formatConversation` signature and body:

```go
func formatConversation(msgs []chatMessage, terms []string) string {
	var b strings.Builder
	for _, m := range msgs {
		text := normalizeText(m.content)
		if m.role == "user" {
			b.WriteString(previewRole.Render("You:"))
			b.WriteString("\n")
			b.WriteString(highlightTerms(previewUser.Render(text), terms))
		} else {
			b.WriteString(previewRole.Render("Claude:"))
			b.WriteString("\n")
			b.WriteString(highlightTerms(previewAsst.Render(text), terms))
		}
		b.WriteString("\n\n")
	}
	return b.String()
}
```

**Step 4: Update updateViewer() to pass matched terms**

Replace `updateViewer()`:

```go
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
```

Note: Remove the `m.selectedIdx != i` optimization from `updateViewer` — we need to re-render when search terms change even if the same session is selected.

**Step 5: Verify it compiles**

Run: `cd /Users/markl/apps/clips2 && go build -o /dev/null .`
Expected: Clean compile

**Step 6: Commit**

```bash
git add main.go
git commit -m "Add yellow background highlighting of matched search terms in preview"
```

---

### Task 5: Build, Test Manually, Final Commit

**Files:**
- Modify: none (manual testing)

**Step 1: Build the binary**

Run: `cd /Users/markl/apps/clips2 && go build -o clip .`

**Step 2: Test manually**

Run: `./clip`

Test these scenarios:
1. Empty search — all sessions shown, no highlighting
2. Type a word that appears in conversation content but NOT in titles — verify matching sessions appear
3. Select a matching session — verify yellow background highlights on matched terms in preview
4. Clear search — verify all sessions return, no highlighting
5. Type a title-only match — verify it still works

**Step 3: Final commit**

```bash
git add -A
git commit -m "Bleve full-text search with term highlighting"
```
