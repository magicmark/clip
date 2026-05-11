package main

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	safeShellWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)
	snippetWords  = regexp.MustCompile(`\S+`)

	searchCLIHeader = lipgloss.NewStyle().Foreground(accent).Bold(true)
	searchCLITitle  = lipgloss.NewStyle().Foreground(base2).Bold(true)
	searchCLILabel  = lipgloss.NewStyle().Foreground(base00).Bold(true)
	searchCLIMuted  = lipgloss.NewStyle().Foreground(base00)
	searchCLIPath   = lipgloss.NewStyle().Foreground(base1)
	searchCLICmd    = lipgloss.NewStyle().Foreground(cyan)
	searchCLIMatch  = lipgloss.NewStyle().Background(highlightColor).Foreground(base03).Bold(true)
)

func runSearchCLI(query string, w io.Writer) int {
	sessions := loadSessions()
	matchedIDs, _ := searchLoadedSessions(sessions, query)
	if len(matchedIDs) == 0 {
		return 1
	}

	var matches []session
	for _, s := range sessions {
		if _, ok := matchedIDs[s.searchKey()]; !ok {
			continue
		}
		matches = append(matches, s)
	}

	fmt.Fprintln(w, searchCLIHeader.Render(searchSummary(len(matches), query)))
	for i, s := range matches {
		marker := fmt.Sprintf("%d.", i+1)
		metadataIndent := strings.Repeat(" ", lipgloss.Width(marker)+1)

		fmt.Fprintln(w)
		fmt.Fprintf(w, "%s %s\n", searchCLIMuted.Render(marker), searchCLITitle.Render(s.title))
		printSearchCLIMetadata(w, metadataIndent, "Source", searchCLIPath.Render(s.sourceLabel()))
		printSearchCLIMetadata(w, metadataIndent, "Directory", searchCLIPath.Render(displayDirectory(s.directory)))
		printSearchCLIMetadata(w, metadataIndent, "Match", highlightSnippet(sessionSnippet(s, query), query))
		printSearchCLIMetadata(w, metadataIndent, "Command", searchCLICmd.Render(resumeCommand(s)))
	}
	return 0
}

func printSearchCLIMetadata(w io.Writer, indent, label, value string) {
	fmt.Fprintf(w, "%s%s  %s\n", indent, searchCLILabel.Render(fmt.Sprintf("%-9s", label)), value)
}

func searchSummary(matches int, query string) string {
	if matches == 1 {
		return fmt.Sprintf("1 match for %q", query)
	}
	return fmt.Sprintf("%d matches for %q", matches, query)
}

func displayDirectory(dir string) string {
	if dir == "" {
		return "(unknown)"
	}
	return dir
}

func sessionSnippet(s session, query string) string {
	q := normalizedSearchQuery(query)
	candidates := []struct {
		label string
		text  string
	}{
		{"Title:", s.title},
		{"Directory:", s.directory},
		{"Path:", searchableSessionPath(s)},
		{"ID:", s.rawID()},
	}
	for _, candidate := range candidates {
		if snippet, ok := matchingSnippet(candidate.label, candidate.text, q); ok {
			return snippet
		}
	}
	for _, m := range s.messages {
		label := s.sourceLabel() + ":"
		if m.role == "user" {
			label = "You:"
		}
		if snippet, ok := matchingSnippet(label, m.content, q); ok {
			return snippet
		}
	}
	return truncate(s.title, 180)
}

func matchingSnippet(label, text, lowerQuery string) (string, bool) {
	if lowerQuery == "" || !strings.Contains(strings.ToLower(text), lowerQuery) {
		return "", false
	}
	snippet := snippetAroundMatch(text, lowerQuery, 6)
	if label == "" {
		return snippet, true
	}
	return label + " " + snippet, true
}

func highlightMatches(text, query string) string {
	lowerQuery := normalizedSearchQuery(query)
	if lowerQuery == "" {
		return text
	}

	lowerText := strings.ToLower(text)
	var b strings.Builder
	pos := 0
	for {
		idx := strings.Index(lowerText[pos:], lowerQuery)
		if idx < 0 {
			b.WriteString(text[pos:])
			break
		}
		start := pos + idx
		end := start + len(lowerQuery)
		b.WriteString(text[pos:start])
		b.WriteString(searchCLIMatch.Render(text[start:end]))
		pos = end
	}
	return b.String()
}

func highlightSnippet(text, query string) string {
	idx := strings.Index(text, ": ")
	if idx < 0 {
		return highlightMatches(text, query)
	}
	prefixEnd := idx + len(": ")
	return text[:prefixEnd] + highlightMatches(text[prefixEnd:], query)
}

func snippetAroundMatch(text, lowerQuery string, contextWords int) string {
	normalized := strings.Join(strings.Fields(text), " ")
	if normalized == "" {
		return ""
	}
	idx := strings.Index(strings.ToLower(normalized), lowerQuery)
	if idx < 0 {
		return truncate(normalized, 180)
	}

	spans := snippetWords.FindAllStringIndex(normalized, -1)
	if len(spans) == 0 {
		return normalized
	}

	matchEnd := idx + len(lowerQuery)
	firstWord := -1
	lastWord := -1
	for i, span := range spans {
		if span[1] <= idx || span[0] >= matchEnd {
			continue
		}
		if firstWord < 0 {
			firstWord = i
		}
		lastWord = i
	}
	if firstWord < 0 {
		return truncate(normalized, 180)
	}

	startWord := firstWord - contextWords
	if startWord < 0 {
		startWord = 0
	}
	endWord := lastWord + contextWords + 1
	if endWord > len(spans) {
		endWord = len(spans)
	}

	snippet := normalized[spans[startWord][0]:spans[endWord-1][1]]
	if startWord > 0 {
		snippet = "... " + snippet
	}
	if endWord < len(spans) {
		snippet += " ..."
	}
	return snippet
}

func resumeCommand(s session) string {
	args := append([]string{resumeExecutable(s)}, resumeArgs(s)...)
	for i, arg := range args {
		args[i] = shellQuote(arg)
	}
	cmd := strings.Join(args, " ")
	if s.directory == "" {
		return cmd
	}
	return "cd " + shellQuote(s.directory) + " && " + cmd
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if safeShellWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
