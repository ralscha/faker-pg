package faker

import "testing"

func TestFakeDataFlags(t *testing.T) {
	var flags fakeDataFlags
	if err := flags.Set(" email = numerify;###-### "); err != nil {
		t.Fatalf("Set(valid): %v", err)
	}
	if got := flags["email"]; got != "numerify;###-###" {
		t.Fatalf("stored rule = %q", got)
	}
	if flags.String() != "1 configured rule(s)" {
		t.Fatalf("String() = %q", flags.String())
	}

	for _, value := range []string{"", "email", "=email", "email=", "email=not-a-function"} {
		if err := flags.Set(value); err == nil {
			t.Errorf("Set(%q) unexpectedly succeeded", value)
		}
	}
}
