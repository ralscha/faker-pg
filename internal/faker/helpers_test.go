package faker

import (
	"net/url"
	"testing"
)

func TestBuildPgDSNRoundTrip(t *testing.T) {
	form := pgDSNForm{
		Host:     "db.example.com",
		Port:     "6543",
		Database: "app",
		Username: "user@example.com",
		Password: "p@ss word",
		SSLMode:  "require",
	}

	dsn := buildPgDSN(form)
	parsed := parsePgDSNForm(dsn)

	if parsed != form {
		t.Fatalf("parsed DSN mismatch:\n got: %#v\nwant: %#v\nraw:  %s", parsed, form, dsn)
	}
}

func TestBuildPgDSNIPv6Host(t *testing.T) {
	dsn := buildPgDSN(pgDSNForm{
		Host:     "::1",
		Port:     "5432",
		Database: "app",
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	if got := u.Hostname(); got != "::1" {
		t.Fatalf("hostname = %q, want ::1; raw DSN %s", got, dsn)
	}
}

func TestShouldIncludeTable(t *testing.T) {
	includeSchemas := makeStringSet(parseList("public, app"))
	excludeSchemas := makeStringSet(parseList("audit"))
	includeTables := makeStringSet(parseList("users, app.orders"))
	excludeTables := makeStringSet(parseList("public.ignored"))

	tests := []struct {
		name   string
		schema string
		table  string
		want   bool
	}{
		{"included bare table", "public", "users", true},
		{"included qualified table", "app", "orders", true},
		{"excluded schema", "audit", "users", false},
		{"excluded qualified table", "public", "ignored", false},
		{"not in table include list", "public", "profiles", false},
		{"not in schema include list", "other", "users", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldIncludeTable(tt.schema, tt.table, includeSchemas, excludeSchemas, includeTables, excludeTables)
			if got != tt.want {
				t.Fatalf("shouldIncludeTable(%q, %q) = %v, want %v", tt.schema, tt.table, got, tt.want)
			}
		})
	}
}

func TestDataFakerRulePrecedence(t *testing.T) {
	f, err := newDataFaker(map[string]string{
		"email":              "username",
		"users.email":        "firstname",
		"public.users.email": "email",
		`public\..*\.name`:   "lastname",
	})
	if err != nil {
		t.Fatalf("newDataFaker: %v", err)
	}

	table := tableMeta{Schema: "public", Name: "users"}
	rule, ok := f.matchRule(table, columnMeta{Name: "email"})
	if !ok || rule.lookupName != "email" {
		t.Fatalf("full selector rule = (%q, %v), want email/true", rule.lookupName, ok)
	}

	rule, ok = f.matchRule(tableMeta{Schema: "app", Name: "users"}, columnMeta{Name: "email"})
	if !ok || rule.lookupName != "firstname" {
		t.Fatalf("table selector rule = (%q, %v), want firstname/true", rule.lookupName, ok)
	}

	rule, ok = f.matchRule(tableMeta{Schema: "app", Name: "profiles"}, columnMeta{Name: "email"})
	if !ok || rule.lookupName != "username" {
		t.Fatalf("column selector rule = (%q, %v), want username/true", rule.lookupName, ok)
	}

	rule, ok = f.matchRule(table, columnMeta{Name: "name"})
	if !ok || rule.lookupName != "lastname" {
		t.Fatalf("regex selector rule = (%q, %v), want lastname/true", rule.lookupName, ok)
	}
}

func TestTruncateStringCountsRunes(t *testing.T) {
	if got := truncateString("\u00e5bc\U0001f600d", 4); got != "\u00e5bc\U0001f600" {
		t.Fatalf("truncateString counted bytes: got %q", got)
	}
}
