package faker

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

type tuiScreen int

const (
	tuiScreenForm tuiScreen = iota
	tuiScreenLoadingSchema
	tuiScreenFakeData
	tuiScreenFakerPicker
	tuiScreenFakerParams
	tuiScreenAutoSelecting
	tuiScreenExecuting
	tuiScreenConfirmRemote
	tuiScreenExecutionResult
)

const formFieldCount = 17

type schemaLoadedMsg struct {
	entries []tuiFakeDataEntry
	err     error
}

type autoSelectDoneMsg struct {
	selections map[string]string
	err        error
}

type executionDoneMsg struct {
	tableCount int
	rowCount   int64
	logLines   []string
	err        error
}

type tuiModel struct {
	cfg    config
	screen tuiScreen

	formInputs []textinput.Model

	fakeDataTable table.Model

	pickerInput  textinput.Model
	pickerCursor int

	paramTarget int
	paramInput  textinput.Model
	paramOption fakeFunctionOption

	fakeDataEntries []tuiFakeDataEntry
	fakeFunctions   []fakeFunctionOption
	spinner         spinner.Model
	status          string
	width           int
	height          int
	quitting        bool
	runInProgress   bool
	pendingFakeCfg  config
	formAction      int // 0=text inputs, 1=edit button, 2=start button
	logLines        []string
	logScroll       int
}

func newTUIModel(cfg config) tuiModel {
	inputs := make([]textinput.Model, formFieldCount)
	placeholderSty := lipgloss.NewStyle().Foreground(colorMuted)
	for i := range inputs {
		inputs[i] = textinput.New()
		inputs[i].Prompt = "  "
		inputs[i].SetWidth(40)
	}

	fields := []struct {
		placeholder string
		value       string
	}{
		{pgDefaultHost, ""},
		{"5432", "5432"},
		{"mydb", ""},
		{pgSchemePostgres, ""},
		{"", ""},
		{pgDefaultSSLMode, pgDefaultSSLMode},
		{"public,app", strings.Join(cfg.IncludeSchemas, ",")},
		{"pg_catalog,information_schema", strings.Join(cfg.ExcludeSchemas, ",")},
		{"", strings.Join(cfg.IncludeTables, ",")},
		{"", strings.Join(cfg.ExcludeTables, ",")},
		{llmProviderOpenAI, cfg.LLM.Provider},
		{"gpt-5.4-mini", cfg.LLM.Model},
		{"https://api.openai.com/v1", cfg.LLM.BaseURL},
		{"", cfg.LLM.APIKey},
		{"OPENAI_API_KEY", cfg.LLM.APIKeyEnv},
		{"1000", fmt.Sprintf("%d", max(1, cfg.BatchSize))},
		{"1", fmt.Sprintf("%d", max(1, cfg.Workers))},
	}
	for i, f := range fields {
		if f.placeholder != "" {
			inputs[i].Placeholder = f.placeholder
		}
		if f.value != "" {
			inputs[i].SetValue(f.value)
		}
	}

	inputs[4].EchoMode = textinput.EchoPassword
	inputs[4].EchoCharacter = rune(8226)
	inputs[13].EchoMode = textinput.EchoPassword
	inputs[13].EchoCharacter = rune(8226)

	focusedStyle := textinput.DefaultStyles(true)
	focusedStyle.Focused.Prompt = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	focusedStyle.Focused.Text = lipgloss.NewStyle().Foreground(lipgloss.White)
	focusedStyle.Blurred.Placeholder = placeholderSty
	focusedStyle.Focused.Placeholder = placeholderSty
	for i := range inputs {
		inputs[i].SetStyles(focusedStyle)
	}

	inputs[0].Focus()

	tableStyles := table.DefaultStyles()
	tableStyles.Header = lipgloss.NewStyle().
		Foreground(colorPrimary).
		Bold(true).
		Padding(0, 1).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorSubtle)
	tableStyles.Cell = lipgloss.NewStyle().Padding(0, 1)
	tableStyles.Selected = lipgloss.NewStyle().
		Foreground(lipgloss.White).
		Background(colorPrimary).
		Bold(true)

	fakeDataTable := table.New(
		table.WithColumns([]table.Column{
			{Title: "Column", Width: 48},
			{Title: "Type", Width: 20},
			{Title: "Faker Function", Width: 36},
		}),
		table.WithFocused(true),
		table.WithHeight(20),
		table.WithStyles(tableStyles),
	)

	pickerInput := textinput.New()
	pickerInput.Placeholder = "type to filter..."
	pickerInput.Prompt = "  search: "
	pickerInput.SetWidth(40)
	pickerInput.Focus()

	paramInput := textinput.New()
	paramInput.Placeholder = "value"
	paramInput.Prompt = "  > "
	paramInput.SetWidth(40)
	paramInput.Focus()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	return tuiModel{
		cfg:           cfg,
		screen:        tuiScreenForm,
		formInputs:    inputs,
		fakeDataTable: fakeDataTable,
		pickerInput:   pickerInput,
		paramInput:    paramInput,
		fakeFunctions: availableFakeFunctionOptions(),
		spinner:       s,
		width:         100,
		height:        30,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.formInputs[0].Focus())
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.fakeDataTable.SetWidth(max(1, msg.Width-4))
		m.fakeDataTable.SetHeight(max(1, msg.Height-12))
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case schemaLoadedMsg:
		if msg.err != nil {
			m.screen = tuiScreenForm
			m.status = msg.err.Error()
			return m, nil
		}
		m.screen = tuiScreenFakeData
		m.fakeDataEntries = msg.entries
		m.rebuildFakeDataTable()
		if len(msg.entries) == 0 {
			m.status = "No copyable columns were found."
		} else {
			m.status = fmt.Sprintf("Loaded %d columns for fake-data editing.", len(msg.entries))
		}
		return m, nil

	case autoSelectDoneMsg:
		m.screen = tuiScreenFakeData
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		applied := 0
		byName := make(map[string]fakeFunctionOption, len(m.fakeFunctions))
		for _, option := range m.fakeFunctions {
			byName[option.LookupName] = option
		}
		for index, entry := range m.fakeDataEntries {
			name, ok := msg.selections[entry.Selector]
			if !ok {
				continue
			}
			option, ok := byName[name]
			if !ok {
				continue
			}
			m.fakeDataEntries[index].FunctionName = option.LookupName
			m.fakeDataEntries[index].FunctionDisplay = option.Display
			m.fakeDataEntries[index].FunctionParams = nil
			applied++
		}
		m.rebuildFakeDataTable()
		m.status = fmt.Sprintf("Applied %d LLM faker suggestions.", applied)
		return m, nil

	case executionDoneMsg:
		m.runInProgress = false
		m.logLines = msg.logLines
		m.logScroll = 0
		if msg.err != nil {
			m.status = fmt.Sprintf("Execution failed: %v", msg.err)
		} else {
			m.status = fmt.Sprintf("Done! Updated %d rows across %d tables.", msg.rowCount, msg.tableCount)
		}
		m.screen = tuiScreenExecutionResult
		return m, nil

	case tea.PasteMsg:
		switch m.screen {
		case tuiScreenForm:
			for i := range m.formInputs {
				if m.formInputs[i].Focused() {
					var cmd tea.Cmd
					m.formInputs[i], cmd = m.formInputs[i].Update(msg)
					return m, cmd
				}
			}
		case tuiScreenFakerPicker:
			var cmd tea.Cmd
			m.pickerInput, cmd = m.pickerInput.Update(msg)
			return m, cmd
		case tuiScreenFakerParams:
			var cmd tea.Cmd
			m.paramInput, cmd = m.paramInput.Update(msg)
			return m, cmd
		case tuiScreenLoadingSchema, tuiScreenFakeData, tuiScreenAutoSelecting, tuiScreenExecuting, tuiScreenConfirmRemote, tuiScreenExecutionResult:
			// paste is ignored in these screens
		}

	case tea.KeyPressMsg:
		if m.quitting {
			return m, nil
		}
		switch m.screen {
		case tuiScreenForm:
			return m.updateForm(msg)
		case tuiScreenLoadingSchema, tuiScreenAutoSelecting, tuiScreenExecuting:
			if msg.String() == keyCtrlC || msg.String() == "q" {
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		case tuiScreenExecutionResult:
			return m.updateExecutionResult(msg)
		case tuiScreenFakeData:
			return m.updateFakeData(msg)
		case tuiScreenFakerPicker:
			return m.updateFakerPicker(msg)
		case tuiScreenFakerParams:
			return m.updateFakerParams(msg)
		case tuiScreenConfirmRemote:
			return m.updateConfirmRemote(msg)
		}
	}

	if m.screen == tuiScreenFakeData {
		if _, ok := msg.(tea.KeyMsg); ok {
			var cmd tea.Cmd
			m.fakeDataTable, cmd = m.fakeDataTable.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m tuiModel) View() tea.View {
	var b strings.Builder

	b.WriteString(titleStyle.Render("faker-pg -- PostgreSQL Fake Data Anonymizer"))
	b.WriteString("\n")
	b.WriteString(m.statusView())
	b.WriteString("\n\n")

	switch m.screen {
	case tuiScreenForm:
		b.WriteString(m.formView())
	case tuiScreenLoadingSchema:
		b.WriteString(m.workingView("Connecting to PostgreSQL and loading schema..."))
	case tuiScreenFakeData:
		b.WriteString(m.fakeDataView())
	case tuiScreenFakerPicker:
		b.WriteString(m.fakerPickerView())
	case tuiScreenFakerParams:
		b.WriteString(m.fakerParamsView())
	case tuiScreenAutoSelecting:
		b.WriteString(m.workingView("Analyzing columns with the configured LLM..."))
	case tuiScreenExecuting:
		b.WriteString(m.workingView("Replacing sensitive data with faked values..."))
	case tuiScreenConfirmRemote:
		b.WriteString(m.confirmRemoteView())
	case tuiScreenExecutionResult:
		b.WriteString(m.executionResultView())
	}

	view := tea.NewView(b.String())
	view.AltScreen = true
	return view
}

func (m tuiModel) statusView() string {
	if strings.TrimSpace(m.status) == "" {
		return statusStyle.Render("  Status: ready")
	}
	lowerStatus := strings.ToLower(m.status)
	if strings.Contains(lowerStatus, "fail") || strings.Contains(lowerStatus, "error") ||
		strings.Contains(lowerStatus, "required") || strings.Contains(lowerStatus, "invalid") {
		return statusErrStyle.Render("  Status: " + m.status)
	}
	return statusOKStyle.Render("  Status: " + m.status)
}

func (m tuiModel) workingView(message string) string {
	return fmt.Sprintf("  %s %s\n\n%s",
		m.spinner.View(),
		message,
		helpStyle.Render("  Press ctrl+c or q to cancel."),
	)
}

func (m tuiModel) formView() string {
	var b strings.Builder

	b.WriteString(sectionHeaderStyle.Render("  PostgreSQL Connection"))
	b.WriteString("\n")
	b.WriteString(m.formRow("Host", m.formInputs[0]))
	b.WriteString(m.formRow("Port", m.formInputs[1]))
	b.WriteString(m.formRow("Database", m.formInputs[2]))
	b.WriteString(m.formRow("User", m.formInputs[3]))
	b.WriteString(m.formRow("Password", m.formInputs[4]))
	b.WriteString(m.formRow("SSL Mode", m.formInputs[5]))
	b.WriteString(m.formRow("Include schemas", m.formInputs[6]))
	b.WriteString(m.formRow("Exclude schemas", m.formInputs[7]))
	b.WriteString(m.formRow("Include tables", m.formInputs[8]))
	b.WriteString(m.formRow("Exclude tables", m.formInputs[9]))

	b.WriteString("\n")
	b.WriteString(sectionHeaderStyle.Render("  LLM Configuration"))
	b.WriteString("\n")
	b.WriteString(m.formRow("LLM Provider", m.formInputs[10]))
	b.WriteString(m.formRow("LLM Model", m.formInputs[11]))
	b.WriteString(m.formRow("LLM Base URL", m.formInputs[12]))
	b.WriteString(m.formRow("LLM API Key", m.formInputs[13]))
	b.WriteString(m.formRow("LLM API Key Env", m.formInputs[14]))

	b.WriteString("\n")
	b.WriteString(sectionHeaderStyle.Render("  Execution"))
	b.WriteString("\n")
	b.WriteString(m.formRow("Batch size", m.formInputs[15]))
	b.WriteString(m.formRow("Workers", m.formInputs[16]))

	fakeCount := len(m.cfg.FakeData)
	editLabel := fmt.Sprintf("[^F] Edit fake data (%d rules)", fakeCount)
	startLabel := "[^A] Start anonymization"
	editBtn := buttonStyle.Render(editLabel)
	startBtn := buttonStyle.Render(startLabel)
	switch m.formAction {
	case 1:
		editBtn = activeButtonStyle.Render(editLabel)
	case 2:
		startBtn = activeButtonStyle.Render(startLabel)
	}

	b.WriteString("\n")
	b.WriteString("  " + editBtn + "  " + startBtn)
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  Keys: type to edit, tab/↑↓ navigate, ^F=edit rules, ^A=start, ctrl+c quits."))

	return b.String()
}

func (m tuiModel) formRow(labelText string, input textinput.Model) string {
	lbl := labelStyle.Render(fmt.Sprintf("%-20s", labelText))
	if input.Focused() {
		lbl = activeLabelStyle.Render(fmt.Sprintf("%-20s", labelText))
	}
	return fmt.Sprintf("  %s %s\n", lbl, input.View())
}

func (m tuiModel) fakeDataView() string {
	if len(m.fakeDataEntries) == 0 {
		return "  No columns found.\n\n" + helpStyle.Render("  Press 'q' to go back.")
	}

	var b strings.Builder
	b.WriteString(m.fakeDataTable.View())
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  up/dn navigate  |  enter pick faker  |  x clear  |  a auto-select LLM  |  q back"))
	return b.String()
}

func (m tuiModel) fakerPickerView() string {
	var b strings.Builder

	target := m.pickerTarget()
	if target >= 0 && target < len(m.fakeDataEntries) {
		entry := m.fakeDataEntries[target]
		b.WriteString(sectionHeaderStyle.Render(fmt.Sprintf("  Column: %s  (%s)", entry.Display, entry.TypeName)))
		b.WriteString("\n")
	}

	b.WriteString("  " + m.pickerInput.View())
	b.WriteString("\n\n")

	filtered := m.filteredFakeFunctions()
	start, end := visiblePickerRange(m.pickerCursor, len(filtered), m.visibleRows())
	if start > 0 {
		more := lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("... %d earlier functions", start))
		fmt.Fprintf(&b, "  %s\n", more)
	}

	for i := start; i < end; i++ {
		opt := filtered[i]
		cursor := "  "
		if i == m.pickerCursor {
			cursor = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render("> ")
		}
		name := lipgloss.NewStyle().Foreground(colorAccent).Width(30).Render(opt.LookupName)
		desc := lipgloss.NewStyle().Foreground(colorMuted).Render(opt.Display)
		fmt.Fprintf(&b, "  %s%s %s\n", cursor, name, desc)
	}

	if end < len(filtered) {
		more := lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("... and %d more functions", len(filtered)-end))
		fmt.Fprintf(&b, "\n  %s\n", more)
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  type to filter  |  up/dn navigate  |  enter select  |  esc cancel"))
	return b.String()
}

func (m tuiModel) fakerParamsView() string {
	var b strings.Builder
	b.WriteString(sectionHeaderStyle.Render(fmt.Sprintf("  Configure parameters for %s", m.paramOption.LookupName)))
	b.WriteString("\n\n")

	for _, param := range m.paramOption.Params {
		lbl := lipgloss.NewStyle().Foreground(colorAccent).Render(param.Field + " (" + param.Type + ")")
		desc := lipgloss.NewStyle().Foreground(colorMuted).Render(param.Description)
		fmt.Fprintf(&b, "  %s: %s\n", lbl, desc)
	}

	b.WriteString("\n")
	fmt.Fprintf(&b, "  Values (; separated): %s\n", m.paramInput.View())
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  Enter values in the order shown, separated by ';'. Press esc to cancel."))
	return b.String()
}

func (m tuiModel) confirmRemoteView() string {
	host := strings.TrimSpace(m.formInputs[0].Value())
	msg := fmt.Sprintf("Target host %q is not local.\n\nAre you sure you want to continue?", host)

	var b strings.Builder
	b.WriteString(confirmStyle.Render(msg))
	b.WriteString("\n\n")
	b.WriteString("  " + buttonStyle.Render("[Y] Yes, continue") + "  ")
	b.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Background(colorSubtle).Padding(0, 3).Render("[N] No, cancel"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  Press ctrl+c or q to quit."))
	return b.String()
}

func (m tuiModel) visibleRows() int {
	return max(8, m.height-12)
}

func visiblePickerRange(cursor, total, visible int) (int, int) {
	if total <= 0 || visible <= 0 {
		return 0, 0
	}
	cursor = min(max(0, cursor), total-1)
	start := max(0, cursor-visible+1)
	return start, min(start+visible, total)
}

func (m tuiModel) pickerTarget() int {
	return m.fakeDataTable.Cursor()
}

func (m *tuiModel) rebuildFakeDataTable() {
	rows := make([]table.Row, len(m.fakeDataEntries))
	for i, entry := range m.fakeDataEntries {
		fakerName := "-"
		if entry.FunctionDisplay != "" {
			fakerName = entry.FunctionDisplay
			if len(entry.FunctionParams) > 0 {
				fakerName += "; " + strings.Join(entry.FunctionParams, ";")
			}
		}
		rows[i] = table.Row{entry.Display, entry.TypeName, fakerName}
	}
	m.fakeDataTable.SetRows(rows)
}

func (m tuiModel) filteredFakeFunctions() []fakeFunctionOption {
	typeName := ""
	target := m.pickerTarget()
	if target >= 0 && target < len(m.fakeDataEntries) {
		typeName = m.fakeDataEntries[target].TypeName
	}

	query := strings.ToLower(m.pickerInput.Value())
	var filtered []fakeFunctionOption
	for _, opt := range m.fakeFunctions {
		if !fakeFunctionCompatible(typeName, opt.LookupName, opt.Output) {
			continue
		}
		if query == "" || strings.Contains(opt.SearchText, query) {
			filtered = append(filtered, opt)
		}
	}
	return filtered
}

func (m tuiModel) updateForm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyCtrlC:
		if m.runInProgress {
			m.status = "Wait for the current action to finish before quitting."
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case "tab":
		return m.cycleFormFocus(1), nil
	case "shift+tab":
		return m.cycleFormFocus(-1), nil
	case "down":
		return m.cycleFormFocus(1), nil
	case "up":
		return m.cycleFormFocus(-1), nil
	case keyEnter:
		return m.handleFormEnter()
	case "left":
		if m.formAction > 0 {
			m.formAction--
		}
		return m, nil
	case "right":
		if m.formAction > 0 {
			m.formAction++
			if m.formAction > 2 {
				m.formAction = 2
			}
		}
		return m, nil
	case "ctrl+f":
		m.formAction = 1
		return m.handleFormEnter()
	case "ctrl+a":
		m.formAction = 2
		return m.handleFormEnter()
	}

	for i := range m.formInputs {
		if m.formInputs[i].Focused() {
			var cmd tea.Cmd
			m.formInputs[i], cmd = m.formInputs[i].Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m tuiModel) cycleFormFocus(direction int) tuiModel {
	current := -1
	for i := range m.formInputs {
		if m.formInputs[i].Focused() {
			current = i
			break
		}
	}

	if direction > 0 {
		if m.formAction > 0 {
			if m.formAction < 2 {
				m.formAction++
			}
		} else if current >= 0 {
			m.formInputs[current].Blur()
			if current < formFieldCount-1 {
				m.formInputs[current+1].Focus()
			} else {
				m.formAction = 1
			}
		}
	} else {
		switch {
		case m.formAction > 1:
			m.formAction--
		case m.formAction == 1:
			m.formAction = 0
			m.formInputs[formFieldCount-1].Focus()
		case current > 0:
			m.formInputs[current].Blur()
			m.formInputs[current-1].Focus()
		}
	}
	return m
}

func (m tuiModel) handleFormEnter() (tea.Model, tea.Cmd) {
	cfg, err := m.configFromForm()
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.cfg = cfg

	if m.formAction == 2 {
		if len(cfg.FakeData) == 0 {
			if cached, found, err := loadCachedMappings(cfg.DSN); err != nil {
				m.status = err.Error()
				return m, nil
			} else if found {
				cfg.FakeData = cached
				m.cfg.FakeData = cached
			}
		}
		if len(cfg.FakeData) == 0 {
			m.status = "No fake data rules configured. Edit fake data first."
			return m, nil
		}
		if !isLocalHost(m.formInputs[0].Value()) {
			m.pendingFakeCfg = cfg
			m.screen = tuiScreenConfirmRemote
			m.status = ""
			return m, nil
		}
		m.runInProgress = true
		m.screen = tuiScreenExecuting
		m.status = "Starting anonymization..."
		return m, executeAnonymizationCmd(cfg)
	}

	m.screen = tuiScreenLoadingSchema
	m.status = "Connecting to PostgreSQL to discover schema..."
	return m, loadSchemaCmd(cfg)
}

func (m *tuiModel) configFromForm() (config, error) {
	host := strings.TrimSpace(m.formInputs[0].Value())
	if host == "" {
		return config{}, fmt.Errorf("host is required")
	}
	db := strings.TrimSpace(m.formInputs[2].Value())
	if db == "" {
		return config{}, fmt.Errorf("database is required")
	}

	form := pgDSNForm{
		Host:     host,
		Port:     strings.TrimSpace(m.formInputs[1].Value()),
		Database: db,
		Username: strings.TrimSpace(m.formInputs[3].Value()),
		Password: m.formInputs[4].Value(),
		SSLMode:  strings.TrimSpace(m.formInputs[5].Value()),
		Options:  parsePgDSNForm(m.cfg.DSN).Options,
	}
	dsn := buildPgDSN(form)

	cfg := config{
		DSN:            dsn,
		IncludeSchemas: parseList(m.formInputs[6].Value()),
		ExcludeSchemas: parseList(m.formInputs[7].Value()),
		IncludeTables:  parseList(m.formInputs[8].Value()),
		ExcludeTables:  parseList(m.formInputs[9].Value()),
		BatchSize:      1000,
		LLM: llmConfig{
			Provider:  strings.TrimSpace(m.formInputs[10].Value()),
			Model:     strings.TrimSpace(m.formInputs[11].Value()),
			BaseURL:   strings.TrimSpace(m.formInputs[12].Value()),
			APIKey:    strings.TrimSpace(m.formInputs[13].Value()),
			APIKeyEnv: strings.TrimSpace(m.formInputs[14].Value()),
		},
		Verbose: m.cfg.Verbose,
	}
	cfg.LLM = normalizeLLMConfig(&cfg.LLM)

	if bs := strings.TrimSpace(m.formInputs[15].Value()); bs != "" {
		n, err := parsePositiveInt("batch size", bs)
		if err != nil {
			return config{}, err
		}
		cfg.BatchSize = n
	}

	cfg.Workers = 1
	if ws := strings.TrimSpace(m.formInputs[16].Value()); ws != "" {
		n, err := parsePositiveInt("workers", ws)
		if err != nil {
			return config{}, err
		}
		cfg.Workers = n
	}

	cfg.FakeData = m.cfg.FakeData

	return cfg, nil
}

func (m *tuiModel) syncFakeDataIntoConfig() {
	m.cfg.FakeData = entriesToMappings(m.fakeDataEntries)
}

func (m tuiModel) updateFakeData(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	count := len(m.fakeDataEntries)
	if count == 0 {
		switch msg.String() {
		case keyCtrlC, "q":
			m.screen = tuiScreenForm
			m.syncFakeDataIntoConfig()
			return m, nil
		}
		return m, nil
	}

	switch msg.String() {
	case keyCtrlC, "q":
		m.screen = tuiScreenForm
		m.syncFakeDataIntoConfig()
		if err := saveCachedEntries(m.cfg.DSN, m.fakeDataEntries); err != nil {
			m.status = "Warning: " + err.Error()
		} else {
			m.status = "Saved mapping to cache."
		}
		return m, nil
	case keyEnter:
		cursor := m.fakeDataTable.Cursor()
		if cursor >= 0 && cursor < len(m.fakeDataEntries) {
			m.screen = tuiScreenFakerPicker
			m.pickerInput.Reset()
			m.pickerCursor = 0
		}
		return m, nil
	case "x", "delete":
		cursor := m.fakeDataTable.Cursor()
		if cursor >= 0 && cursor < len(m.fakeDataEntries) {
			m.fakeDataEntries[cursor].FunctionName = ""
			m.fakeDataEntries[cursor].FunctionDisplay = ""
			m.fakeDataEntries[cursor].FunctionParams = nil
			m.rebuildFakeDataTable()
			m.status = "Cleared the faker selection for the active column."
		}
		return m, nil
	case "a":
		if m.cfg.LLM.isConfigured() {
			m.screen = tuiScreenAutoSelecting
			unmapped := make([]tuiFakeDataEntry, 0, len(m.fakeDataEntries))
			for _, e := range m.fakeDataEntries {
				if e.FunctionName == "" {
					unmapped = append(unmapped, e)
				}
			}
			entries := m.fakeDataEntries
			if len(unmapped) > 0 {
				entries = unmapped
			}
			return m, autoSelectCmd(m.cfg.LLM, entries, m.fakeFunctions)
		}
		m.status = "LLM is not configured. Set Model and API Key first."
		return m, nil
	}

	var cmd tea.Cmd
	m.fakeDataTable, cmd = m.fakeDataTable.Update(msg)
	return m, cmd
}

func (m tuiModel) updateFakerPicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredFakeFunctions()

	switch msg.String() {
	case keyCtrlC, keyEsc:
		m.screen = tuiScreenFakeData
		return m, nil
	case "up":
		if m.pickerCursor > 0 {
			m.pickerCursor--
		}
		return m, nil
	case "down":
		if m.pickerCursor < len(filtered)-1 {
			m.pickerCursor++
		}
		return m, nil
	case keyEnter:
		if m.pickerCursor >= 0 && m.pickerCursor < len(filtered) {
			selected := filtered[m.pickerCursor]
			target := m.pickerTarget()
			if target >= 0 && target < len(m.fakeDataEntries) {
				if len(selected.Params) > 0 {
					m.paramTarget = target
					m.paramOption = selected
					m.paramInput.Reset()
					m.screen = tuiScreenFakerParams
					return m, nil
				}

				entry := &m.fakeDataEntries[target]
				entry.FunctionName = selected.LookupName
				entry.FunctionDisplay = selected.Display
				entry.FunctionParams = nil
				m.screen = tuiScreenFakeData
				m.syncFakeDataIntoConfig()
				m.rebuildFakeDataTable()
				m.status = fmt.Sprintf("Set %s -> %s", entry.Selector, selected.Display)
			}
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.pickerInput, cmd = m.pickerInput.Update(msg)
	m.pickerCursor = 0
	return m, cmd
}

func (m tuiModel) updateFakerParams(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyCtrlC, keyEsc:
		m.screen = tuiScreenFakeData
		return m, nil
	case keyEnter:
		entry := &m.fakeDataEntries[m.paramTarget]
		params := parseParameterValues(m.paramInput.Value())
		functionConfig := buildFakeFunctionConfig(m.paramOption.LookupName, params)
		if _, _, err := compileFakeDataRule(entry.Selector, functionConfig); err != nil {
			m.status = "Invalid parameters: " + err.Error()
			return m, nil
		}
		entry.FunctionName = m.paramOption.LookupName
		entry.FunctionDisplay = m.paramOption.Display
		entry.FunctionParams = params
		m.screen = tuiScreenFakeData
		m.syncFakeDataIntoConfig()
		m.rebuildFakeDataTable()
		m.status = fmt.Sprintf("Set %s -> %s", entry.Selector, entry.FunctionDisplay)
		return m, nil
	}

	var cmd tea.Cmd
	m.paramInput, cmd = m.paramInput.Update(msg)
	return m, cmd
}

func (m tuiModel) updateConfirmRemote(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(msg.String()) {
	case "y":
		m.runInProgress = true
		m.screen = tuiScreenExecuting
		m.status = "Starting anonymization..."
		return m, executeAnonymizationCmd(m.pendingFakeCfg)
	case "n", keyEsc:
		m.screen = tuiScreenForm
		m.pendingFakeCfg = config{}
		m.status = "Anonymization cancelled."
		return m, nil
	case keyCtrlC, "q":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m tuiModel) updateExecutionResult(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyCtrlC, "q":
		m.quitting = true
		return m, tea.Quit
	case keyEnter, keyEsc:
		m.screen = tuiScreenFakeData
		return m, nil
	case "up", "k":
		if m.logScroll > 0 {
			m.logScroll--
		}
	case "down", "j":
		visibleRows := max(4, m.height-12)
		maxScroll := max(0, len(m.logLines)-visibleRows)
		if m.logScroll < maxScroll {
			m.logScroll++
		}
	}
	return m, nil
}

func (m tuiModel) executionResultView() string {
	var b strings.Builder
	b.WriteString(sectionHeaderStyle.Render("  Execution Log"))
	b.WriteString("\n")

	visibleRows := max(4, m.height-12)
	start := m.logScroll
	end := min(start+visibleRows, len(m.logLines))

	checkMark := lipgloss.NewStyle().Foreground(colorAccent).Render("✓")
	if len(m.logLines) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("  (no log output)"))
		b.WriteString("\n")
	} else {
		for _, line := range m.logLines[start:end] {
			fmt.Fprintf(&b, "  %s %s\n", checkMark, line)
		}
		if len(m.logLines) > visibleRows {
			scrollInfo := fmt.Sprintf("  %d–%d of %d lines", start+1, end, len(m.logLines))
			b.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(scrollInfo))
			b.WriteString("\n")
		}
	}

	b.WriteString(helpStyle.Render("  up/dn or j/k scroll  |  enter or esc back to data table  |  q quits"))
	return b.String()
}

func loadSchemaCmd(cfg config) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
		if err != nil {
			return schemaLoadedMsg{err: fmt.Errorf("parse DSN: %w", err)}
		}
		poolCfg.MaxConns = 4
		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
		if err != nil {
			return schemaLoadedMsg{err: fmt.Errorf("connect: %w", err)}
		}
		defer pool.Close()

		tables, err := loadTables(ctx, pool, cfg.IncludeSchemas, cfg.ExcludeSchemas, cfg.IncludeTables, cfg.ExcludeTables)
		if err != nil {
			return schemaLoadedMsg{err: err}
		}

		var entries []tuiFakeDataEntry
		for _, tbl := range tables {
			for _, col := range tbl.Columns {
				if !col.Copyable {
					continue
				}
				selector := buildSelector(tbl.Schema, tbl.Name, col.Name)
				entry := tuiFakeDataEntry{
					Selector: selector,
					Display:  tbl.Schema + "." + tbl.Name + "." + col.Name,
					TypeName: fakerTypeName(col),
				}
				entries = append(entries, entry)
			}
		}

		cached, found, err := loadCachedEntries(cfg.DSN)
		if err != nil {
			return schemaLoadedMsg{err: err}
		}
		if found && len(cached) > 0 {
			cachedBySelector := make(map[string]tuiFakeDataEntry, len(cached))
			for _, c := range cached {
				cachedBySelector[c.Selector] = c
			}
			for i, entry := range entries {
				if c, ok := cachedBySelector[entry.Selector]; ok {
					entries[i].FunctionName = c.FunctionName
					entries[i].FunctionDisplay = c.FunctionDisplay
					entries[i].FunctionParams = c.FunctionParams
				}
			}
		}
		mappings := cfg.FakeData
		if len(mappings) == 0 {
			cachedMappings, found, err := loadCachedMappings(cfg.DSN)
			if err != nil {
				return schemaLoadedMsg{err: err}
			}
			if found {
				mappings = cachedMappings
			}
		}
		if err := applyMappingsToEntries(entries, mappings, availableFakeFunctionOptions()); err != nil {
			return schemaLoadedMsg{err: fmt.Errorf("apply fake-data mappings: %w", err)}
		}

		return schemaLoadedMsg{entries: entries}
	}
}

func autoSelectCmd(llmCfg llmConfig, entries []tuiFakeDataEntry, options []fakeFunctionOption) tea.Cmd {
	return func() tea.Msg {
		selections, err := autoSelectFakeDataWithLLM(context.Background(), llmCfg, entries, options)
		return autoSelectDoneMsg{selections: selections, err: err}
	}
}

func applyMappingsToEntries(entries []tuiFakeDataEntry, mappings map[string]string, options []fakeFunctionOption) error {
	if len(mappings) == 0 {
		return nil
	}
	dataFaker, err := newDataFaker(mappings)
	if err != nil {
		return err
	}
	byName := make(map[string]fakeFunctionOption, len(options))
	for _, option := range options {
		byName[option.LookupName] = option
	}
	for i := range entries {
		parts := strings.Split(entries[i].Selector, ".")
		if len(parts) != 3 {
			continue
		}
		rule, ok := dataFaker.matchRule(
			tableMeta{Schema: parts[0], Name: parts[1]},
			columnMeta{Name: parts[2]},
		)
		if !ok {
			continue
		}
		option, ok := byName[rule.lookupName]
		if !ok {
			continue
		}
		if !fakeFunctionCompatible(entries[i].TypeName, option.LookupName, option.Output) {
			return fmt.Errorf(
				"rule %q produces %s, which is incompatible with %s (%s)",
				rule.selector, option.Output, entries[i].Display, entries[i].TypeName,
			)
		}
		entries[i].FunctionName = option.LookupName
		entries[i].FunctionDisplay = option.Display
		entries[i].FunctionParams = append([]string(nil), rule.rawParams...)
	}
	return nil
}

func executeAnonymizationCmd(cfg config) tea.Cmd {
	return func() tea.Msg {
		result := runAnonymization(context.Background(), cfg)
		return executionDoneMsg(result)
	}
}

func parsePositiveInt(field, value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", field)
	}
	return n, nil
}

func parseParameterValues(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ";")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func (t tableMeta) CopyColumns() []columnMeta {
	var cols []columnMeta
	for _, col := range t.Columns {
		if col.Copyable {
			cols = append(cols, col)
		}
	}
	return cols
}

func (t tableMeta) PrimaryKeyColumns() []string {
	if t.PrimaryKey == nil {
		return nil
	}
	return t.PrimaryKey.Columns
}

func runTUI(cfg config) error {
	model := newTUIModel(cfg)
	if cfg.DSN != "" {
		form := parsePgDSNForm(cfg.DSN)
		model.formInputs[0].SetValue(form.Host)
		model.formInputs[1].SetValue(form.Port)
		model.formInputs[2].SetValue(form.Database)
		model.formInputs[3].SetValue(form.Username)
		model.formInputs[4].SetValue(form.Password)
		model.formInputs[5].SetValue(form.SSLMode)
	}

	program := tea.NewProgram(model)
	_, err := program.Run()
	return err
}
