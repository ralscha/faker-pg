package faker

import (
	"testing"

	"charm.land/bubbles/v2/textinput"
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
