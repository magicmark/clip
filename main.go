package main

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	focusSearch = iota
	focusTable
	focusEditor
)

type model struct {
	search  textinput.Model
	table   table.Model
	editor  textarea.Model
	allRows []table.Row
	focus   int
	width   int
	height  int
	ready   bool
}

func initialModel() model {
	columns := []table.Column{
		{Title: "Rank", Width: 4},
		{Title: "City", Width: 10},
		{Title: "Country", Width: 10},
		{Title: "Population", Width: 10},
	}

	rows := []table.Row{
		{"1", "Tokyo", "Japan", "37,274,000"},
		{"2", "Delhi", "India", "32,065,760"},
		{"3", "Shanghai", "China", "28,516,904"},
		{"4", "Dhaka", "Bangladesh", "22,478,116"},
		{"5", "São Paulo", "Brazil", "22,429,800"},
		{"6", "Mexico City", "Mexico", "22,085,140"},
		{"7", "Cairo", "Egypt", "21,750,020"},
		{"8", "Beijing", "China", "21,333,332"},
		{"9", "Mumbai", "India", "20,961,472"},
		{"10", "Osaka", "Japan", "19,059,856"},
		{"11", "Chongqing", "China", "16,874,740"},
		{"12", "Karachi", "Pakistan", "16,839,950"},
		{"13", "Istanbul", "Turkey", "15,636,243"},
		{"14", "Kinshasa", "DR Congo", "15,628,085"},
		{"15", "Lagos", "Nigeria", "15,387,639"},
		{"16", "Buenos Aires", "Argentina", "15,369,919"},
		{"17", "Kolkata", "India", "15,133,888"},
		{"18", "Manila", "Philippines", "14,406,059"},
		{"19", "Tianjin", "China", "14,011,828"},
		{"20", "Guangzhou", "China", "13,964,637"},
		{"21", "Rio De Janeiro", "Brazil", "13,634,274"},
		{"22", "Lahore", "Pakistan", "13,541,764"},
		{"23", "Bangalore", "India", "13,193,035"},
		{"24", "Shenzhen", "China", "12,831,330"},
		{"25", "Moscow", "Russia", "12,640,818"},
		{"26", "Chennai", "India", "11,503,293"},
		{"27", "Bogota", "Colombia", "11,344,312"},
		{"28", "Paris", "France", "11,142,303"},
		{"29", "Jakarta", "Indonesia", "11,074,811"},
		{"30", "Lima", "Peru", "11,044,607"},
		{"31", "Bangkok", "Thailand", "10,899,698"},
		{"32", "Hyderabad", "India", "10,534,418"},
		{"33", "Seoul", "South Korea", "9,975,709"},
		{"34", "Nagoya", "Japan", "9,571,596"},
		{"35", "London", "United Kingdom", "9,540,576"},
		{"36", "Chengdu", "China", "9,478,521"},
		{"37", "Nanjing", "China", "9,429,381"},
		{"38", "Tehran", "Iran", "9,381,546"},
		{"39", "Ho Chi Minh City", "Vietnam", "9,077,158"},
		{"40", "Luanda", "Angola", "8,952,496"},
		{"41", "Wuhan", "China", "8,591,611"},
		{"42", "Xi An Shaanxi", "China", "8,537,646"},
		{"43", "Ahmedabad", "India", "8,450,228"},
		{"44", "Kuala Lumpur", "Malaysia", "8,419,566"},
		{"45", "New York City", "United States", "8,177,020"},
		{"46", "Hangzhou", "China", "8,044,878"},
		{"47", "Surat", "India", "7,784,276"},
		{"48", "Suzhou", "China", "7,764,499"},
		{"49", "Hong Kong", "Hong Kong", "7,643,256"},
		{"50", "Riyadh", "Saudi Arabia", "7,538,200"},
		{"51", "Shenyang", "China", "7,527,975"},
		{"52", "Baghdad", "Iraq", "7,511,920"},
		{"53", "Dongguan", "China", "7,511,851"},
		{"54", "Foshan", "China", "7,497,263"},
		{"55", "Dar Es Salaam", "Tanzania", "7,404,689"},
		{"56", "Pune", "India", "6,987,077"},
		{"57", "Santiago", "Chile", "6,856,939"},
		{"58", "Madrid", "Spain", "6,713,557"},
		{"59", "Haerbin", "China", "6,665,951"},
		{"60", "Toronto", "Canada", "6,312,974"},
		{"61", "Belo Horizonte", "Brazil", "6,194,292"},
		{"62", "Khartoum", "Sudan", "6,160,327"},
		{"63", "Johannesburg", "South Africa", "6,065,354"},
		{"64", "Singapore", "Singapore", "6,039,577"},
		{"65", "Dalian", "China", "5,930,140"},
		{"66", "Qingdao", "China", "5,865,232"},
		{"67", "Zhengzhou", "China", "5,690,312"},
		{"68", "Ji Nan Shandong", "China", "5,663,015"},
		{"69", "Barcelona", "Spain", "5,658,472"},
		{"70", "Saint Petersburg", "Russia", "5,535,556"},
		{"71", "Abidjan", "Ivory Coast", "5,515,790"},
		{"72", "Yangon", "Myanmar", "5,514,454"},
		{"73", "Fukuoka", "Japan", "5,502,591"},
		{"74", "Alexandria", "Egypt", "5,483,605"},
		{"75", "Guadalajara", "Mexico", "5,339,583"},
		{"76", "Ankara", "Turkey", "5,309,690"},
		{"77", "Chittagong", "Bangladesh", "5,252,842"},
		{"78", "Addis Ababa", "Ethiopia", "5,227,794"},
		{"79", "Melbourne", "Australia", "5,150,766"},
		{"80", "Nairobi", "Kenya", "5,118,844"},
		{"81", "Hanoi", "Vietnam", "5,067,352"},
		{"82", "Sydney", "Australia", "5,056,571"},
		{"83", "Monterrey", "Mexico", "5,036,535"},
		{"84", "Changsha", "China", "4,809,887"},
		{"85", "Brasilia", "Brazil", "4,803,877"},
		{"86", "Cape Town", "South Africa", "4,800,954"},
		{"87", "Jiddah", "Saudi Arabia", "4,780,740"},
		{"88", "Urumqi", "China", "4,710,203"},
		{"89", "Kunming", "China", "4,657,381"},
		{"90", "Changchun", "China", "4,616,002"},
		{"91", "Hefei", "China", "4,496,456"},
		{"92", "Shantou", "China", "4,490,411"},
		{"93", "Xinbei", "Taiwan", "4,470,672"},
		{"94", "Kabul", "Afghanistan", "4,457,882"},
		{"95", "Ningbo", "China", "4,405,292"},
		{"96", "Tel Aviv", "Israel", "4,343,584"},
		{"97", "Yaounde", "Cameroon", "4,336,670"},
		{"98", "Rome", "Italy", "4,297,877"},
		{"99", "Shijiazhuang", "China", "4,285,135"},
		{"100", "Montreal", "Canada", "4,276,526"},
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

	ta := textarea.New()
	ta.Placeholder = "Write something..."
	ta.ShowLineNumbers = false

	si := textinput.New()
	si.Prompt = " / "
	si.Placeholder = "Filter cities..."
	// Unbind tab from accepting suggestions so it can switch panes
	si.KeyMap.AcceptSuggestion = key.NewBinding(key.WithDisabled())

	return model{
		search:  si,
		table:   t,
		editor:  ta,
		allRows: rows,
		focus:   focusSearch,
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

func (m model) Init() tea.Cmd {
	return nil
}

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
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		cmds = append(cmds, cmd)
	case focusEditor:
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) applyFocus(cmds *[]tea.Cmd) {
	m.search.Blur()
	m.editor.Blur()
	m.table.Blur()

	switch m.focus {
	case focusSearch:
		*cmds = append(*cmds, m.search.Focus())
	case focusTable:
		m.table.Focus()
	case focusEditor:
		*cmds = append(*cmds, m.editor.Focus())
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
		if strings.Contains(strings.ToLower(row[1]), query) {
			filtered = append(filtered, row)
		}
	}
	m.table.SetRows(filtered)
}

func (m *model) updateSizes() {
	if !m.ready {
		return
	}
	// Layout: search bar (3 rows with border) + panes + help (1 row)
	searchHeight := 3
	contentHeight := m.height - searchHeight - 1

	halfWidth := m.width / 2

	m.search.SetWidth(m.width - 6) // account for border + prompt

	// Table: left half (border adds 2)
	tableInner := halfWidth - 2
	m.table.SetWidth(tableInner)
	m.table.SetHeight(contentHeight - 2)

	// Distribute column widths to fill the table
	cols := m.table.Columns()
	if len(cols) > 0 {
		gap := len(cols) + 1 // table adds 1-char gaps between columns and edges
		available := tableInner - gap
		each := available / len(cols)
		remainder := available % len(cols)
		for i := range cols {
			cols[i].Width = each
			if i < remainder {
				cols[i].Width++
			}
		}
		m.table.SetColumns(cols)
	}

	// Editor: right half (border adds 2)
	editorWidth := m.width - halfWidth - 2
	m.editor.SetWidth(editorWidth)
	m.editor.SetHeight(contentHeight - 2)
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
	if m.focus == focusTable {
		leftBorder = activeBorder
		rightBorder = inactiveBorder
	} else if m.focus == focusEditor {
		leftBorder = inactiveBorder
		rightBorder = activeBorder
	} else {
		leftBorder = inactiveBorder
		rightBorder = inactiveBorder
	}

	left := leftBorder.Render(m.table.View())
	right := rightBorder.Render(m.editor.View())
	panes := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	help := helpStyle.Render("  tab: switch pane • /: search • ctrl+c: quit")

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
