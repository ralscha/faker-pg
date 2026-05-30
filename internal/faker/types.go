package faker

import "fmt"

type tableMeta struct {
	Schema      string
	Name        string
	Columns     []columnMeta
	PrimaryKey  *keyConstraint
	ForeignKeys []foreignKey
}

func (t tableMeta) FQTN() string {
	return fmt.Sprintf("%q.%q", t.Schema, t.Name)
}

type columnMeta struct {
	Name       string
	DataType   string // e.g. "character varying", "integer", "text", "timestamp with time zone"
	UDTName    string // e.g. "varchar", "int4", "text", "timestamptz"
	MaxLength  int    // for varchar, bpchar; -1 if not applicable
	Precision  int
	Scale      int
	Nullable   bool
	IsArray    bool
	OrdinalPos int
	Copyable   bool
	SkipReason string
}

type keyConstraint struct {
	Name    string
	Columns []string
}

type foreignKey struct {
	Name         string
	Columns      []string
	RefSchema    string
	RefTable     string
	RefColumns   []string
	DeleteAction string
	UpdateAction string
}

type pgDSNForm struct {
	Host     string
	Port     string
	Database string
	Username string
	Password string
	SSLMode  string
}

type config struct {
	DSN            string
	IncludeSchemas []string
	ExcludeSchemas []string
	IncludeTables  []string
	ExcludeTables  []string
	FakeData       map[string]string // selector -> gofakeit function name
	LLM            llmConfig
	Verbose        bool
	BatchSize      int
	Workers        int
}

type tuiFakeDataEntry struct {
	Selector        string   // e.g. "public.users.email"
	Display         string   // human-readable label
	TypeName        string   // PostgreSQL data type
	FunctionName    string   // gofakeit lookup name
	FunctionDisplay string   // human-readable function name
	FunctionParams  []string // optional parameters
}

type fakeFunctionOption struct {
	LookupName  string
	Display     string
	Category    string
	Description string
	Example     string
	SearchText  string
	Output      string // gofakeit output type, e.g. "string", "int", "bool", "time.Time"
	Params      []fakeParam
}

type fakeParam struct {
	Field       string
	Description string
	Type        string
	Optional    bool
}
