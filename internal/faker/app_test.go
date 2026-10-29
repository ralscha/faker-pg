package faker

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestParseFlags(t *testing.T) {
	cfg, showVersion, err := parseFlags([]string{
		"--include-schemas", " Public, app ", "--exclude-tables", "audit, public.logs",
		"--batch-size", "23", "--workers", "4", "--verbose",
		"--fake-data", "email=email", "--fake-data", "phone=phone",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if showVersion || cfg.BatchSize != 23 || cfg.Workers != 4 || !cfg.Verbose {
		t.Fatalf("unexpected parsed configuration: %#v", cfg)
	}
	if !reflect.DeepEqual(cfg.IncludeSchemas, []string{"public", "app"}) ||
		!reflect.DeepEqual(cfg.ExcludeTables, []string{"audit", "public.logs"}) ||
		!reflect.DeepEqual(cfg.FakeData, map[string]string{"email": "email", "phone": "phone"}) {
		t.Fatalf("filters or mappings were lost: %#v", cfg)
	}
	// Parsing a second invocation must not retain flags or register them globally.
	defaults, _, err := parseFlags(nil, io.Discard)
	if err != nil || defaults.BatchSize != 1000 || defaults.Workers != 1 || len(defaults.FakeData) != 0 {
		t.Fatalf("default configuration = %#v, err = %v", defaults, err)
	}
}

func TestParseFlagsRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"--batch-size", "0"}, {"--batch-size", "-1"}, {"--workers", "0"},
		{"--workers", "-3"}, {"--unknown"}, {"unexpected"},
		{"--dsn", "postgres://user:super-secret@localhost:bad/app"},
	} {
		if _, _, err := parseFlags(args, io.Discard); err == nil {
			t.Errorf("parseFlags(%q) unexpectedly succeeded", args)
		} else if strings.Contains(err.Error(), "super-secret") {
			t.Error("parse error leaked credentials")
		}
	}
}

func TestRunAppHelpAndVersionDoNotStartTUI(t *testing.T) {
	for _, flag := range []string{"--help", "--version"} {
		var stdout, stderr bytes.Buffer
		if err := runApp([]string{flag}, &stdout, &stderr); err != nil {
			t.Fatalf("runApp(%s): %v", flag, err)
		}
		if flag == "--version" && !strings.HasPrefix(stdout.String(), "faker-pg ") {
			t.Fatalf("version output = %q", stdout.String())
		}
		if flag == "--help" && !strings.Contains(stderr.String(), "--") && !strings.Contains(stderr.String(), "-dsn") {
			t.Fatalf("help output = %q", stderr.String())
		}
	}
}

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
