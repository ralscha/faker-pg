package faker

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestRegexSelectorsMatchEntireColumnName(t *testing.T) {
	faker, err := newDataFaker(map[string]string{
		`public\.users\.email|public\.users\.phone`: "email",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		schema string
		column string
		want   bool
	}{
		{"public", "email", true},
		{"PUBLIC", "PHONE", true},
		{"public", "email_extra", false},
		{"notpublic", "phone", false},
	} {
		_, got := faker.matchRule(tableMeta{Schema: tt.schema, Name: "users"}, columnMeta{Name: tt.column})
		if got != tt.want {
			t.Errorf("match %s.users.%s = %v, want %v", tt.schema, tt.column, got, tt.want)
		}
	}
}

func TestRegexSelectorsPreserveEscapeSemantics(t *testing.T) {
	for _, selector := range []string{
		`public\.users\.\D+`,
		`public\.users\.\S+`,
		`public\.users\.\p{L}+`,
	} {
		t.Run(selector, func(t *testing.T) {
			faker, err := newDataFaker(map[string]string{selector: "email"})
			if err != nil {
				t.Fatal(err)
			}
			if _, matched := faker.matchRule(tableMeta{Schema: "public", Name: "users"}, columnMeta{Name: "email"}); !matched {
				t.Fatal("regex did not match email")
			}
		})
	}
}

func TestReplaceTimeValuesCanBeEncodedByPGX(t *testing.T) {
	value := time.Date(2025, time.March, 4, 12, 34, 56, 123456789, time.FixedZone("offset", 90*60))
	for _, tt := range []struct {
		udt  string
		oid  uint32
		want string
	}{
		{"time", pgtype.TimeOID, "12:34:56.123456"},
		{"timetz", pgtype.TimetzOID, "12:34:56.123456+01:30:00"},
	} {
		t.Run(tt.udt, func(t *testing.T) {
			converted := replaceValue(columnMeta{UDTName: tt.udt}, value)
			encoded, err := pgtype.NewMap().Encode(tt.oid, pgtype.TextFormatCode, converted, nil)
			if err != nil {
				t.Fatalf("encode %s: %v", tt.udt, err)
			}
			if string(encoded) != tt.want {
				t.Fatalf("encoded = %q, want %q", encoded, tt.want)
			}
			if got := replaceValue(columnMeta{UDTName: tt.udt}, nil); got != nil {
				t.Fatalf("NULL converted to %#v", got)
			}
		})
	}
}

func TestTimeTypeAliasesAcceptDateGenerators(t *testing.T) {
	for _, dataType := range []string{"time", "timetz", "time without time zone", "time with time zone"} {
		if !fakeFunctionCompatible(dataType, "pastdate", "time") {
			t.Errorf("%s rejects time output", dataType)
		}
	}
}
