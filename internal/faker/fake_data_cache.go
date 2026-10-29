package faker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
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

func saveCachedEntries(dsn string, entries []tuiFakeDataEntry) error {
	cacheKey := pgDSNCacheKey(dsn)
	if cacheKey == "" {
		return nil
	}

	path, err := cachePath()
	if err != nil {
		return err
	}

	persisted, err := readMappingCache(path)
	if err != nil {
		return err
	}

	if persisted.Sources == nil {
		persisted.Sources = make(map[string][]persistedEntry)
	}
	persisted.Sources[cacheKey] = encodeEntries(entries)
	if persisted.Mappings == nil {
		persisted.Mappings = make(map[string]map[string]string)
	}
	// Retain empty mappings so clearing the editor cannot revive legacy rules.
	persisted.Mappings[cacheKey] = entriesToMappings(entries)
	return writeMappingCache(path, persisted)
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
	persisted, err := readMappingCache(path)
	if err != nil {
		return nil, false, err
	}
	for _, key := range pgDSNCacheKeys(dsn) {
		if mappings, ok := persisted.Mappings[key]; ok {
			return cloneStringMap(mappings), true, nil
		}
		if entries, ok := persisted.Sources[key]; ok {
			return entriesToMappings(decodeEntries(entries)), true, nil
		}
	}
	return nil, false, nil
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

	persisted, err := readMappingCache(path)
	if err != nil {
		return err
	}

	if persisted.Mappings == nil {
		persisted.Mappings = make(map[string]map[string]string)
	}
	persisted.Mappings[cacheKey] = cloneStringMap(mappings)
	delete(persisted.Sources, cacheKey)
	return writeMappingCache(path, persisted)
}

func readMappingCache(path string) (persistedMappings, error) {
	persisted := persistedMappings{}
	raw, err := os.ReadFile(path) //nolint:gosec // path is derived from os.UserHomeDir(), not user input
	if os.IsNotExist(err) {
		return persisted, nil
	}
	if err != nil {
		return persisted, fmt.Errorf("read cache %q: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, &persisted); err != nil {
		return persisted, fmt.Errorf("parse cache %q: %w", path, err)
	}
	return persisted, nil
}

func writeMappingCache(path string, persisted persistedMappings) error {
	encoded, err := yaml.Marshal(persisted)
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cache dir %q: %w", filepath.Dir(path), err)
	}
	// Write the complete replacement beside the old file before renaming it,
	// so a failed write does not destroy previously saved mappings.
	temp, err := os.CreateTemp(filepath.Dir(path), ".mapping-*")
	if err != nil {
		return fmt.Errorf("create temporary cache: %w", err)
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	if _, err := temp.Write(encoded); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write cache %q: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close cache %q: %w", path, err)
	}
	if err := os.Rename(temp.Name(), path); err != nil {
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
