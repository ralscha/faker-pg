package faker

// PostgreSQL DSN / connection defaults.
const (
	pgSchemePostgres   = "postgres"
	pgSchemePostgreSQL = "postgresql"
	pgDefaultHost      = "localhost"
	pgDefaultSSLMode   = "disable"
)

// LLM provider names.
const (
	llmProviderOpenAI = "openai"
)

// Shared PostgreSQL / gofakeit data-type tokens.
const (
	dtName = "name"
	dtUUID = "uuid"
	dtInt  = "int"
	dtInt8 = "int8"
)

// TUI key strings.
const (
	keyCtrlC = "ctrl+c"
	keyEnter = "enter"
	keyEsc   = "esc"
)
