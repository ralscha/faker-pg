package faker

import (
	"flag"
	"fmt"
	"log"
	"strings"
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
		fakeData       fakeDataFlags
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
	flag.Var(&fakeData, "fake-data", "fake-data rule in selector=function[;parameter...] form (repeatable)")
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
		FakeData:       cloneStringMap(fakeData),
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

type fakeDataFlags map[string]string

func (f *fakeDataFlags) String() string {
	if f == nil || len(*f) == 0 {
		return ""
	}
	return fmt.Sprintf("%d configured rule(s)", len(*f))
}

func (f *fakeDataFlags) Set(value string) error {
	selector, functionConfig, ok := strings.Cut(value, "=")
	selector = strings.TrimSpace(selector)
	functionConfig = strings.TrimSpace(functionConfig)
	if !ok || selector == "" || functionConfig == "" {
		return fmt.Errorf("fake-data rule must use selector=function[;parameter...] syntax")
	}
	if _, _, err := compileFakeDataRule(selector, functionConfig); err != nil {
		return err
	}
	if *f == nil {
		*f = make(fakeDataFlags)
	}
	(*f)[selector] = functionConfig
	return nil
}
