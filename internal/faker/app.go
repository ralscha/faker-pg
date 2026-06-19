package faker

import (
	"flag"
	"log"
)

func Main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if err := runMain(); err != nil {
		log.Fatal(err)
	}
}

func runMain() error {
	cfg := parseFlags()
	return runTUI(cfg)
}

func parseFlags() config {
	var (
		dsn            string
		includeSchemas string
		excludeSchemas string
		includeTables  string
		excludeTables  string
		llmModel       string
		llmBaseURL     string
		llmAPIKey      string
		llmAPIKeyEnv   string
		llmProvider    string
		batchSize      int
		workers        int
		verbose        bool
	)

	flag.StringVar(&dsn, "dsn", "", "PostgreSQL DSN (postgres://user:pass@host:port/db?sslmode=disable)")
	flag.StringVar(&includeSchemas, "include-schemas", "", "comma-separated schemas to include")
	flag.StringVar(&excludeSchemas, "exclude-schemas", "", "comma-separated schemas to exclude")
	flag.StringVar(&includeTables, "include-tables", "", "comma-separated tables to include")
	flag.StringVar(&excludeTables, "exclude-tables", "", "comma-separated tables to exclude")
	flag.StringVar(&llmProvider, "llm-provider", "openai", "LLM provider (openai)")
	flag.StringVar(&llmModel, "llm-model", "", "LLM model name")
	flag.StringVar(&llmBaseURL, "llm-base-url", "", "LLM API base URL")
	flag.StringVar(&llmAPIKey, "llm-api-key", "", "LLM API key")
	flag.StringVar(&llmAPIKeyEnv, "llm-api-key-env", "OPENAI_API_KEY", "environment variable for LLM API key")
	flag.IntVar(&batchSize, "batch-size", 1000, "batch size for updates")
	flag.IntVar(&workers, "workers", 1, "number of concurrent workers")
	flag.BoolVar(&verbose, "verbose", false, "verbose logging")
	flag.Parse()

	if batchSize <= 0 {
		batchSize = 1000
	}
	if workers <= 0 {
		workers = 1
	}

	return config{
		DSN:            dsn,
		IncludeSchemas: parseList(includeSchemas),
		ExcludeSchemas: parseList(excludeSchemas),
		IncludeTables:  parseList(includeTables),
		ExcludeTables:  parseList(excludeTables),
		BatchSize:      batchSize,
		Workers:        workers,
		Verbose:        verbose,
		LLM: normalizeLLMConfig(&llmConfig{
			Provider:  llmProvider,
			Model:     llmModel,
			BaseURL:   llmBaseURL,
			APIKey:    llmAPIKey,
			APIKeyEnv: llmAPIKeyEnv,
		}),
	}
}
