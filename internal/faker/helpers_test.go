package faker

import (
	"net/url"
	"reflect"
	"strings"
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

	if !reflect.DeepEqual(parsed, form) {
		t.Fatalf("parsed DSN mismatch:\n got: %#v\nwant: %#v\nraw:  %s", parsed, form, dsn)
	}
}

func TestBuildPgDSNPreservesPasswordWhitespace(t *testing.T) {
	form := pgDSNForm{Host: "localhost", Database: "app", Username: "user", Password: " secret "}
	parsed := parsePgDSNForm(buildPgDSN(form))
	if parsed.Password != form.Password {
		t.Fatalf("password whitespace was changed: got %q", parsed.Password)
	}
}

func TestPgDSNPreservesConnectionOptions(t *testing.T) {
	input := "postgres://user:pass@db.example.com:5432/app?sslmode=verify-full&application_name=faker-pg&connect_timeout=10" //nolint:gosec // synthetic test DSN
	parsed := parsePgDSNForm(input)
	if parsed.Options.Get("application_name") != "faker-pg" || parsed.Options.Get("connect_timeout") != "10" {
		t.Fatalf("parsed options = %#v", parsed.Options)
	}
	built := buildPgDSN(parsed)
	u, err := url.Parse(built)
	if err != nil {
		t.Fatalf("parse rebuilt DSN: %v", err)
	}
	if got := u.Query().Get("sslmode"); got != "verify-full" {
		t.Fatalf("sslmode = %q", got)
	}
	if got := u.Query().Get("application_name"); got != "faker-pg" {
		t.Fatalf("application_name = %q", got)
	}
}

func TestParsePgKeyValueDSNHandlesQuotedValues(t *testing.T) {
	parsed := parsePgDSNForm(`host=db.example.com port=5432 dbname='my app' user=test password='p\'ass word' application_name='faker pg'`)
	if parsed.Database != "my app" || parsed.Password != "p'ass word" {
		t.Fatalf("parsed quoted values: %#v", parsed)
	}
	if got := parsed.Options.Get("application_name"); got != "faker pg" {
		t.Fatalf("application_name = %q", got)
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

func TestPgDSNCacheKeyIncludesPort(t *testing.T) {
	first := pgDSNCacheKey("postgres://localhost:5432/app")
	second := pgDSNCacheKey("postgres://localhost:5433/app")
	if first == second {
		t.Fatalf("cache keys collide: %q", first)
	}
	if first != "localhost:5432/app" {
		t.Fatalf("cache key = %q", first)
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

func TestFQTNQuotesPostgreSQLIdentifiers(t *testing.T) {
	table := tableMeta{Schema: `odd"schema`, Name: "table name"}
	if got, want := table.FQTN(), `"odd""schema"."table name"`; got != want {
		t.Fatalf("FQTN() = %q, want %q", got, want)
	}
}

func TestMarkUnsafeColumns(t *testing.T) {
	table := tableMeta{
		PrimaryKey:  &keyConstraint{Columns: []string{"id"}},
		ForeignKeys: []foreignKey{{Columns: []string{"account_id"}}},
		Referenced:  []string{"external_code"},
		Columns: []columnMeta{
			{Name: "id", Copyable: true},
			{Name: "account_id", Copyable: true},
			{Name: "email", Copyable: true},
			{Name: "external_code", Copyable: true},
			{Name: "payload", Copyable: false, SkipReason: "unsupported type: jsonb"},
		},
	}

	markUnsafeColumns(&table)
	if table.Columns[0].Copyable || !strings.Contains(table.Columns[0].SkipReason, "primary key") {
		t.Fatalf("primary key was not protected: %#v", table.Columns[0])
	}
	if table.Columns[1].Copyable || !strings.Contains(table.Columns[1].SkipReason, "foreign key") {
		t.Fatalf("foreign key was not protected: %#v", table.Columns[1])
	}
	if !table.Columns[2].Copyable {
		t.Fatalf("ordinary supported column was disabled: %#v", table.Columns[2])
	}
	if table.Columns[3].Copyable || !strings.Contains(table.Columns[3].SkipReason, "referenced by foreign keys") {
		t.Fatalf("referenced key was not protected: %#v", table.Columns[3])
	}
	if table.Columns[4].Copyable || table.Columns[4].SkipReason != "unsupported type: jsonb" {
		t.Fatalf("existing unsupported reason was changed: %#v", table.Columns[4])
	}
}

func TestMarkUnsafeColumnsRejectsTableWithoutPrimaryKey(t *testing.T) {
	table := tableMeta{Columns: []columnMeta{{Name: "email", Copyable: true}}}
	markUnsafeColumns(&table)
	if table.Columns[0].Copyable || !strings.Contains(table.Columns[0].SkipReason, "without a primary key") {
		t.Fatalf("column = %#v, want non-copyable no-PK reason", table.Columns[0])
	}
}

func TestSupportedColumnType(t *testing.T) {
	for _, typeName := range []string{"text", "int4", "numeric", "timestamptz", "uuid", "inet"} {
		if !supportedColumnType(typeName) {
			t.Errorf("supportedColumnType(%q) = false", typeName)
		}
	}
	for _, typeName := range []string{"jsonb", "bytea", "point", "tsvector", "custom_enum"} {
		if supportedColumnType(typeName) {
			t.Errorf("supportedColumnType(%q) = true", typeName)
		}
	}
}
