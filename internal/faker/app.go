package faker

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/ralscha/faker-pg/internal/pgutil"
)

// Set by the release build; go install builds use module build information.
var version = "dev"

func Main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if err := runMain(); err != nil {
		log.Fatal(err)
	}
}

func runMain() error {
	return runApp(os.Args[1:], os.Stdout, os.Stderr)
}

func runApp(args []string, stdout, stderr io.Writer) error {
	cfg, showVersion, err := parseFlags(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if showVersion {
		_, err := fmt.Fprintln(stdout, "faker-pg "+buildVersion())
		return err
	}
	return runTUI(cfg)
}

func buildVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

func parseFlags(args []string, output io.Writer) (config, bool, error) {
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
		showVersion    bool
		fakeData       fakeDataFlags
	)
	flags := flag.NewFlagSet("faker-pg", flag.ContinueOnError)
	flags.SetOutput(output)

	flags.StringVar(&dsn, "dsn", "", "PostgreSQL DSN (postgres://user:pass@host:port/db?sslmode=disable)")
	flags.StringVar(&includeSchemas, "include-schemas", "", "comma-separated schemas to include")
	flags.StringVar(&excludeSchemas, "exclude-schemas", "", "comma-separated schemas to exclude")
	flags.StringVar(&includeTables, "include-tables", "", "comma-separated tables to include")
	flags.StringVar(&excludeTables, "exclude-tables", "", "comma-separated tables to exclude")
	flags.StringVar(&llmProvider, "llm-provider", "openai", "LLM provider (openai)")
	flags.StringVar(&llmModel, "llm-model", "", "LLM model name")
	flags.StringVar(&llmBaseURL, "llm-base-url", "", "LLM API base URL")
	flags.StringVar(&llmAPIKey, "llm-api-key", "", "LLM API key")
	flags.StringVar(&llmAPIKeyEnv, "llm-api-key-env", "OPENAI_API_KEY", "environment variable for LLM API key")
	flags.IntVar(&batchSize, "batch-size", 1000, "batch size for updates (positive integer)")
	flags.IntVar(&workers, "workers", 1, "number of concurrent workers (positive integer)")
	flags.BoolVar(&verbose, "verbose", false, "verbose logging")
	flags.BoolVar(&showVersion, "version", false, "print version and exit")
	flags.Var(&fakeData, "fake-data", "fake-data rule in selector=function[;parameter...] form (repeatable)")
	if err := flags.Parse(args); err != nil {
		return config{}, false, err
	}
	if flags.NArg() > 0 {
		return config{}, false, fmt.Errorf("unexpected positional arguments; use --dsn for the connection string")
	}
	if showVersion {
		return config{}, true, nil
	}

	if batchSize <= 0 {
		return config{}, false, fmt.Errorf("batch-size must be a positive integer")
	}
	if workers <= 0 {
		return config{}, false, fmt.Errorf("workers must be a positive integer")
	}
	if dsn != "" {
		if _, err := pgx.ParseConfig(dsn); err != nil {
			return config{}, false, pgutil.RedactParseError(err)
		}
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
	}, false, nil
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
