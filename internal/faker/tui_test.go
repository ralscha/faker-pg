package faker

import "testing"

func TestApplyMappingsToEntries(t *testing.T) {
	entries := []tuiFakeDataEntry{
		{Selector: "public.users.email", Display: "public.users.email", TypeName: "text"},
		{Selector: "public.users.phone", Display: "public.users.phone", TypeName: "text"},
	}

	applyMappingsToEntries(entries, map[string]string{
		"public.users.email": "Email",
		"public.users.phone": "numerify;###-###",
	}, availableFakeFunctionOptions())

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
