package faker

import "testing"

func TestPreferredSensitiveFunctionsAreUsableWithoutParameters(t *testing.T) {
	options := preferredSensitiveFakeFunctions(availableFakeFunctionOptions())
	byName := make(map[string]fakeFunctionOption, len(options))
	for _, option := range options {
		byName[option.LookupName] = option
		// Auto-selection returns only function names, so each offered option
		// must work without requiring the user to supply missing parameters.
		if _, _, err := compileFakeDataRule("public.users.value", option.LookupName); err != nil {
			t.Errorf("auto-select offers unusable %q: %v", option.LookupName, err)
		}
	}
	for name, dataType := range map[string]string{"phone": "text", "pastdate": "date"} {
		option, ok := byName[name]
		if !ok {
			t.Errorf("auto-select has no %q generator", name)
			continue
		}
		if !fakeFunctionCompatible(dataType, option.LookupName, option.Output) {
			t.Errorf("auto-select %q is incompatible with %s columns", name, dataType)
		}
	}
}
