package faker

import (
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

func TestApplyMappingsToEntries(t *testing.T) {
	entries := []tuiFakeDataEntry{
		{Selector: "public.users.email", Display: "public.users.email", TypeName: "text"},
		{Selector: "public.users.phone", Display: "public.users.phone", TypeName: "text"},
	}

	err := applyMappingsToEntries(entries, map[string]string{
		"public.users.email": "Email",
		"public.users.phone": "numerify;###-###",
	}, availableFakeFunctionOptions())
	if err != nil {
		t.Fatalf("applyMappingsToEntries: %v", err)
	}

	if entries[0].FunctionName != "email" {
		t.Fatalf("email function = %q, want email", entries[0].FunctionName)
	}
	if entries[0].FunctionDisplay == "" {
		t.Fatalf("email function display was not populated")
	}
	if entries[1].FunctionName != "numerify" {
		t.Fatalf("phone function = %q, want numerify", entries[1].FunctionName)
	}
	if len(entries[1].FunctionParams) != 1 || entries[1].FunctionParams[0] != "###-###" {
		t.Fatalf("phone params = %#v, want ###-###", entries[1].FunctionParams)
	}
}

func TestApplyMappingsToEntriesExpandsGenericRules(t *testing.T) {
	entries := []tuiFakeDataEntry{
		{Selector: "public.users.email", TypeName: "text"},
		{Selector: "app.accounts.email", TypeName: "text"},
		{Selector: "app.accounts.name", TypeName: "text"},
	}
	err := applyMappingsToEntries(entries, map[string]string{
		"email":         "email",
		`app\..*\.name`: "name",
	}, availableFakeFunctionOptions())
	if err != nil {
		t.Fatalf("applyMappingsToEntries: %v", err)
	}
	for i, entry := range entries {
		if entry.FunctionName == "" {
			t.Errorf("entry %d was not populated: %#v", i, entry)
		}
	}
}

func TestEntriesToMappings(t *testing.T) {
	mappings := entriesToMappings([]tuiFakeDataEntry{
		{Selector: "public.users.email", FunctionName: "email"},
		{Selector: "public.users.phone", FunctionName: "numerify", FunctionParams: []string{"###-###"}},
		{Selector: "public.users.unmapped"},
	})

	if got := mappings["public.users.email"]; got != "email" {
		t.Fatalf("email mapping = %q, want email", got)
	}
	if got := mappings["public.users.phone"]; got != "numerify;###-###" {
		t.Fatalf("phone mapping = %q, want numerify;###-###", got)
	}
	if _, ok := mappings["public.users.unmapped"]; ok {
		t.Fatalf("unmapped entry should not be persisted")
	}
}

func TestParsePositiveInt(t *testing.T) {
	if got, err := parsePositiveInt("workers", "12"); err != nil || got != 12 {
		t.Fatalf("parsePositiveInt(valid) = %d, %v", got, err)
	}
	for _, value := range []string{"", "0", "-1", "12oops"} {
		if _, err := parsePositiveInt("workers", value); err == nil {
			t.Errorf("parsePositiveInt(%q) unexpectedly succeeded", value)
		}
	}
}

func TestParseParameterValues(t *testing.T) {
	got := parseParameterValues("  first ; second;  ")
	want := []string{"first", "second", ""}
	if len(got) != len(want) {
		t.Fatalf("params = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("params = %#v, want %#v", got, want)
		}
	}
}

func TestTUIHidesSecrets(t *testing.T) {
	model := newTUIModel(config{BatchSize: 1000, Workers: 1})
	for _, index := range []int{4, 13} {
		if model.formInputs[index].EchoMode != textinput.EchoPassword {
			t.Errorf("form input %d is not password-masked", index)
		}
	}
}

func TestPickerDoesNotOfferFunctionsForUnsupportedTypes(t *testing.T) {
	model := newTUIModel(config{BatchSize: 1000, Workers: 1})
	model.fakeDataEntries = []tuiFakeDataEntry{{Selector: "public.docs.payload", TypeName: "jsonb"}}
	model.fakeFunctions = []fakeFunctionOption{{LookupName: "word", Output: "string"}}
	if got := model.filteredFakeFunctions(); len(got) != 0 {
		t.Fatalf("filtered functions = %#v, want none", got)
	}
}

func TestPickerRestrictsStructuredStringTypes(t *testing.T) {
	model := newTUIModel(config{BatchSize: 1000, Workers: 1})
	model.fakeDataEntries = []tuiFakeDataEntry{{Selector: "public.docs.id", TypeName: "uuid"}}
	model.fakeFunctions = []fakeFunctionOption{
		{LookupName: "email", Output: "string"},
		{LookupName: "uuid", Output: "string"},
	}
	got := model.filteredFakeFunctions()
	if len(got) != 1 || got[0].LookupName != "uuid" {
		t.Fatalf("filtered functions = %#v, want only uuid", got)
	}
}

func TestVisiblePickerRangeFollowsCursor(t *testing.T) {
	tests := []struct {
		cursor, total, visible int
		wantStart, wantEnd     int
	}{
		{cursor: 0, total: 20, visible: 5, wantStart: 0, wantEnd: 5},
		{cursor: 8, total: 20, visible: 5, wantStart: 4, wantEnd: 9},
		{cursor: 19, total: 20, visible: 5, wantStart: 15, wantEnd: 20},
		{cursor: 0, total: 0, visible: 5, wantStart: 0, wantEnd: 0},
	}
	for _, tt := range tests {
		start, end := visiblePickerRange(tt.cursor, tt.total, tt.visible)
		if start != tt.wantStart || end != tt.wantEnd {
			t.Errorf("visiblePickerRange(%d, %d, %d) = (%d, %d), want (%d, %d)",
				tt.cursor, tt.total, tt.visible, start, end, tt.wantStart, tt.wantEnd)
		}
	}
}

func TestFormArrowKeysEditText(t *testing.T) {
	model := newTUIModel(config{BatchSize: 1000, Workers: 1})
	model.formInputs[0].SetValue("abcd")
	model.formInputs[0].CursorEnd()
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	model = updated.(tuiModel)
	if got := model.formInputs[0].Position(); got != 3 {
		t.Fatalf("cursor after left = %d, want 3", got)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if got := updated.(tuiModel).formInputs[0].Position(); got != 4 {
		t.Fatalf("cursor after right = %d, want 4", got)
	}
}

func TestShortcutAndButtonNavigationKeepSingleFocus(t *testing.T) {
	model := newTUIModel(config{BatchSize: 1000, Workers: 1})
	// An invalid form stays on the form after the shortcut, with the edit
	// button selected. Navigating back must restore exactly one text field.
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	model = updated.(tuiModel)
	if model.formAction != 1 {
		t.Fatalf("form action = %d, want edit", model.formAction)
	}
	for i, input := range model.formInputs {
		if input.Focused() {
			t.Fatalf("field %d still focused while button is selected", i)
		}
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	model = updated.(tuiModel)
	if model.formAction != 0 || !model.formInputs[formFieldCount-1].Focused() {
		t.Fatal("left from edit button did not restore the last field")
	}
}

func TestPastingPickerFilterResetsSelection(t *testing.T) {
	model := newTUIModel(config{BatchSize: 1000, Workers: 1})
	model.screen = tuiScreenFakerPicker
	model.pickerCursor = 100
	updated, _ := model.Update(tea.PasteMsg{Content: "email"})
	model = updated.(tuiModel)
	if model.pickerCursor != 0 || model.pickerInput.Value() != "email" {
		t.Fatalf("paste left cursor %d with filter %q", model.pickerCursor, model.pickerInput.Value())
	}
}

func TestEditingGeneratorPreservesExistingParameters(t *testing.T) {
	model := newTUIModel(config{BatchSize: 1000, Workers: 1})
	model.screen = tuiScreenFakerPicker
	model.fakeDataEntries = []tuiFakeDataEntry{{
		Selector: "public.users.phone", TypeName: "text", FunctionName: "numerify", FunctionParams: []string{"###-###"},
	}}
	model.fakeFunctions = []fakeFunctionOption{{LookupName: "numerify", Output: "string", Params: []fakeParam{{Field: "format"}}}}
	model.rebuildFakeDataTable()
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(tuiModel)
	if model.screen != tuiScreenFakerParams || model.paramInput.Value() != "###-###" {
		t.Fatal("editing existing generator discarded its parameters")
	}
}

func TestExecutionResultReturnsToForm(t *testing.T) {
	model := newTUIModel(config{BatchSize: 1000, Workers: 1})
	model.screen = tuiScreenExecutionResult
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if updated.(tuiModel).screen != tuiScreenForm {
		t.Fatal("direct execution returned to an unloaded data table")
	}
}
