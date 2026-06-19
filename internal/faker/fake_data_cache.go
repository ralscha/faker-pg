package faker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	cacheDirName  = ".faker-pg"
	cacheFileName = "fake-data-mapping.yml"
)

type persistedMappings struct {
	Sources  map[string][]persistedEntry  `yaml:"sources,omitempty"`
	Mappings map[string]map[string]string `yaml:"mappings,omitempty"`
}

type persistedEntry struct {
	Selector        string   `yaml:"selector,omitempty"`
	Display         string   `yaml:"display,omitempty"`
	TypeName        string   `yaml:"type-name,omitempty"`
	FunctionName    string   `yaml:"function-name,omitempty"`
	FunctionDisplay string   `yaml:"function-display,omitempty"`
	FunctionParams  []string `yaml:"function-params,omitempty"`
}

func cachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, cacheDirName, cacheFileName), nil
}

func loadCachedEntries(dsn string) ([]tuiFakeDataEntry, bool, error) {
	cacheKey := pgDSNCacheKey(dsn)
	if cacheKey == "" {
		return nil, false, nil
	}

	path, err := cachePath()
	if err != nil {
		return nil, false, err
	}
	raw, err := os.ReadFile(path) //nolint:gosec // path is derived from os.UserHomeDir(), not user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read cache %q: %w", path, err)
	}

	var persisted persistedMappings
	if err := yaml.Unmarshal(raw, &persisted); err != nil {
		return nil, false, fmt.Errorf("parse cache %q: %w", path, err)
	}
	entries, ok := persisted.Sources[cacheKey]
	if !ok {
		return nil, false, nil
	}
	return decodeEntries(entries), true, nil
}

func saveCachedEntries(dsn string, entries []tuiFakeDataEntry) error {
	cacheKey := pgDSNCacheKey(dsn)
	if cacheKey == "" {
		return nil
	}

	path, err := cachePath()
	if err != nil {
		return err
	}

	persisted := persistedMappings{}
	raw, err := os.ReadFile(path) //nolint:gosec // path is derived from os.UserHomeDir(), not user input
	if err == nil {
		if unmarshalErr := yaml.Unmarshal(raw, &persisted); unmarshalErr != nil {
			return fmt.Errorf("parse cache %q: %w", path, unmarshalErr)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read cache %q: %w", path, err)
	}

	if persisted.Sources == nil {
		persisted.Sources = make(map[string][]persistedEntry)
	}
	persisted.Sources[cacheKey] = encodeEntries(entries)
	if persisted.Mappings == nil {
		persisted.Mappings = make(map[string]map[string]string)
	}
	mappings := entriesToMappings(entries)
	if len(mappings) == 0 {
		delete(persisted.Mappings, cacheKey)
	} else {
		persisted.Mappings[cacheKey] = mappings
	}

	encoded, err := yaml.Marshal(persisted)
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cache dir %q: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write cache %q: %w", path, err)
	}
	return nil
}

func loadCachedMappings(dsn string) (map[string]string, bool, error) {
	cacheKey := pgDSNCacheKey(dsn)
	if cacheKey == "" {
		return nil, false, nil
	}

	path, err := cachePath()
	if err != nil {
		return nil, false, err
	}
	raw, err := os.ReadFile(path) //nolint:gosec // path is derived from os.UserHomeDir(), not user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read cache %q: %w", path, err)
	}

	var persisted persistedMappings
	if err := yaml.Unmarshal(raw, &persisted); err != nil {
		return nil, false, fmt.Errorf("parse cache %q: %w", path, err)
	}
	mappings, ok := persisted.Mappings[cacheKey]
	if !ok || len(mappings) == 0 {
		return nil, false, nil
	}
	return cloneStringMap(mappings), true, nil
}

func saveCachedMappings(dsn string, mappings map[string]string) error {
	cacheKey := pgDSNCacheKey(dsn)
	if cacheKey == "" {
		return nil
	}

	path, err := cachePath()
	if err != nil {
		return err
	}

	persisted := persistedMappings{}
	raw, err := os.ReadFile(path) //nolint:gosec // path is derived from os.UserHomeDir(), not user input
	if err == nil {
		if unmarshalErr := yaml.Unmarshal(raw, &persisted); unmarshalErr != nil {
			return fmt.Errorf("parse cache %q: %w", path, unmarshalErr)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read cache %q: %w", path, err)
	}

	if persisted.Mappings == nil {
		persisted.Mappings = make(map[string]map[string]string)
	}
	if len(mappings) == 0 {
		delete(persisted.Mappings, cacheKey)
	} else {
		persisted.Mappings[cacheKey] = cloneStringMap(mappings)
	}

	encoded, err := yaml.Marshal(persisted)
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cache dir %q: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write cache %q: %w", path, err)
	}
	return nil
}

func entriesToMappings(entries []tuiFakeDataEntry) map[string]string {
	mappings := make(map[string]string)
	for _, entry := range entries {
		if strings.TrimSpace(entry.FunctionName) == "" {
			continue
		}
		mappings[entry.Selector] = buildFakeFunctionConfigFromEntry(entry)
	}
	return mappings
}

func encodeEntries(entries []tuiFakeDataEntry) []persistedEntry {
	persisted := make([]persistedEntry, 0, len(entries))
	for _, entry := range entries {
		persisted = append(persisted, persistedEntry{
			Selector:        entry.Selector,
			Display:         entry.Display,
			TypeName:        entry.TypeName,
			FunctionName:    entry.FunctionName,
			FunctionDisplay: entry.FunctionDisplay,
			FunctionParams:  append([]string(nil), entry.FunctionParams...),
		})
	}
	return persisted
}

func decodeEntries(entries []persistedEntry) []tuiFakeDataEntry {
	decoded := make([]tuiFakeDataEntry, 0, len(entries))
	for _, entry := range entries {
		decoded = append(decoded, tuiFakeDataEntry{
			Selector:        entry.Selector,
			Display:         entry.Display,
			TypeName:        entry.TypeName,
			FunctionName:    entry.FunctionName,
			FunctionDisplay: entry.FunctionDisplay,
			FunctionParams:  append([]string(nil), entry.FunctionParams...),
		})
	}
	return decoded
}
