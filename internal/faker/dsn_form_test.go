package faker

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestDSNQueryOverridesAreEditable(t *testing.T) {
	dsn := "postgres://old:old@localhost:5432/old?host=remote.example&port=6543&dbname=other&user=new&password=a+b&sslmode=disable" //nolint:gosec // synthetic DSN exercises credential query overrides
	form := parsePgDSNForm(dsn)
	if form.Host != "remote.example" || form.Port != "6543" || form.Database != "other" || form.Username != "new" || form.Password != "a+b" {
		t.Fatalf("query overrides not reflected in connection form: %#v", form)
	}
	if len(form.Options) != 0 {
		t.Fatalf("hidden connection overrides remain: %#v", form.Options)
	}
	if key := pgDSNCacheKey(dsn); key != "remote.example:6543/other" {
		t.Fatalf("cache key uses wrong destination: %q", key)
	}
	form.Host = "localhost"
	form.Port = "5434"
	form.Database = "edited"
	parsed, err := pgx.ParseConfig(buildPgDSN(form))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "localhost" || parsed.Port != 5434 || parsed.Database != "edited" || parsed.Password != "a+b" {
		t.Fatal("editing the form did not change the effective destination")
	}
}

func TestDSNQueryDuplicatesUseLastValue(t *testing.T) {
	form := parsePgDSNForm("postgres:///app?host=localhost&host=remote.example&dbname=first&database=second")
	if form.Host != "remote.example" || form.Database != "second" {
		t.Fatalf("duplicate settings = %#v", form)
	}
}

func TestLegacyHostAddressAlias(t *testing.T) {
	for _, tt := range []struct {
		dsn, host string
	}{
		{"hostaddr=127.0.0.1 dbname=app", "127.0.0.1"},
		{"host=localhost hostaddr=127.0.0.1 dbname=app", "localhost"},
		{"postgres:///app?hostaddr=127.0.0.1", "127.0.0.1"},
		{"postgres://localhost/app?hostaddr=127.0.0.1", "localhost"},
	} {
		form := parsePgDSNForm(tt.dsn)
		if form.Host != tt.host || form.Options.Has("hostaddr") {
			t.Fatalf("hostaddr alias for %q = host %q, options %v", tt.dsn, form.Host, form.Options)
		}
		cfg, err := pgx.ParseConfig(buildPgDSN(form))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Host != tt.host || cfg.RuntimeParams["hostaddr"] != "" {
			t.Fatal("legacy alias was lost or became an invalid server parameter")
		}
	}
}

func TestDSNKeywordAliasesUseLastValue(t *testing.T) {
	for _, dsn := range []string{
		"host=localhost dbname=first database=second",
		"host=localhost database=first dbname=second",
	} {
		if got := parsePgDSNForm(dsn).Database; got != "second" {
			t.Errorf("database for %q = %q, want second", dsn, got)
		}
	}
}

func TestDSNOptionsPreserveSpacesAndPlusSigns(t *testing.T) {
	form := pgDSNForm{
		Host: "localhost", Database: "app", SSLMode: "disable",
		Options: url.Values{"application_name": {"faker pg+review"}},
	}
	dsn := buildPgDSN(form)
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.RuntimeParams["application_name"]; got != "faker pg+review" {
		t.Fatalf("effective application_name = %q", got)
	}
	if got := parsePgDSNForm(dsn).Options.Get("application_name"); got != "faker pg+review" {
		t.Fatalf("form application_name = %q", got)
	}
}

func TestMultiHostDSNRoundTrip(t *testing.T) {
	form := parsePgDSNForm("postgres://localhost:5432,remote.example:6543/app?sslmode=disable")
	if form.Host != "localhost,remote.example" || form.Port != "5432,6543" {
		t.Fatalf("multiple destinations = %#v", form)
	}
	parsed, err := pgx.ParseConfig(buildPgDSN(form))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "localhost" || parsed.Port != 5432 || len(parsed.Fallbacks) != 1 || parsed.Fallbacks[0].Host != "remote.example" || parsed.Fallbacks[0].Port != 6543 {
		t.Fatal("round trip changed multi-host destinations")
	}
}

func TestMultiHostDSNWithDefaultPorts(t *testing.T) {
	for _, tt := range []struct {
		dsn, hosts, ports string
	}{
		{"postgres://localhost:5432,remote.example/app", "localhost,remote.example", "5432,"},
		{"postgres://[::1],remote.example:6543/app", "::1,remote.example", ",6543"},
		{"postgres://%2Fvar%2Frun%2Fpostgresql/app", "/var/run/postgresql", ""},
	} {
		form := parsePgDSNForm(tt.dsn)
		if form.Host != tt.hosts || form.Port != tt.ports {
			t.Errorf("parsePgDSNForm(%q) = host %q port %q", tt.dsn, form.Host, form.Port)
		}
	}
}

func TestDSNSSLQueryAliasPreservesEncryption(t *testing.T) {
	for _, tt := range []struct {
		query, mode string
	}{
		{"ssl=true", "require"},
		{"sslmode=disable&ssl=true", "require"},
		{"ssl=true&sslmode=verify-full", "verify-full"},
	} {
		form := parsePgDSNForm("postgres://localhost/app?" + tt.query)
		if form.SSLMode != tt.mode || form.Options.Has("ssl") {
			t.Errorf("SSL form for %q = mode %q, options %v", tt.query, form.SSLMode, form.Options)
		}
	}
}

func TestRemoteDatabaseHostsChecksEffectiveDestinations(t *testing.T) {
	for _, tt := range []struct {
		name string
		dsn  string
		want []string
	}{
		{"local", "postgres://localhost/app?sslmode=disable", nil},
		{"IPv6", "postgres://[::1]/app?sslmode=disable", nil},
		{"query override", "postgres://localhost/app?host=remote.example&sslmode=disable", []string{"remote.example"}},
		{"fallback", "postgres://localhost,remote.example/app?sslmode=disable", []string{"remote.example"}},
		{"TLS fallbacks deduplicated", "postgres://remote.example/app?sslmode=prefer", []string{"remote.example"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := remoteDatabaseHosts(tt.dsn)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("remote hosts = %v, want %v", got, tt.want)
			}
		})
	}
}
