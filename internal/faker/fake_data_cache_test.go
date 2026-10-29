package faker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCachedMappingsAreSeparatedByPort(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	firstDSN := "postgres://localhost:5432/app"
	secondDSN := "postgres://localhost:5433/app"
	if err := saveCachedMappings(firstDSN, map[string]string{"email": "email"}); err != nil {
		t.Fatalf("save first cache: %v", err)
	}
	if err := saveCachedMappings(secondDSN, map[string]string{"name": "name"}); err != nil {
		t.Fatalf("save second cache: %v", err)
	}

	first, found, err := loadCachedMappings(firstDSN)
	if err != nil || !found {
		t.Fatalf("load first cache = %#v, %v, %v", first, found, err)
	}
	if first["email"] != "email" || len(first) != 1 {
		t.Fatalf("first cache = %#v", first)
	}
	second, found, err := loadCachedMappings(secondDSN)
	if err != nil || !found {
		t.Fatalf("load second cache = %#v, %v, %v", second, found, err)
	}
	if second["name"] != "name" || len(second) != 1 {
		t.Fatalf("second cache = %#v", second)
	}
}

func TestClearedMappingsDoNotRestoreLegacyRules(t *testing.T) {
	for _, save := range []struct {
		name string
		fn   func(string) error
	}{
		{"mappings", func(dsn string) error { return saveCachedMappings(dsn, nil) }},
		{"entries", func(dsn string) error {
			return saveCachedEntries(dsn, []tuiFakeDataEntry{{Selector: "public.users.email"}})
		}},
	} {
		t.Run(save.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("USERPROFILE", home)
			t.Setenv("HOME", home)
			path, err := cachePath()
			if err != nil {
				t.Fatal(err)
			}
			legacy := persistedMappings{Mappings: map[string]map[string]string{
				"localhost/app": {"email": "email"},
			}}
			if err := writeMappingCache(path, legacy); err != nil {
				t.Fatal(err)
			}
			const dsn = "postgres://localhost:5432/app"
			if err := save.fn(dsn); err != nil {
				t.Fatal(err)
			}
			got, found, err := loadCachedMappings(dsn)
			if err != nil || !found || len(got) != 0 {
				t.Fatalf("cleared cache = %#v, %v, %v", got, found, err)
			}
		})
	}
}

func TestCachedSourceEntriesRemainReadable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	path, err := cachePath()
	if err != nil {
		t.Fatal(err)
	}
	legacy := persistedMappings{Sources: map[string][]persistedEntry{
		"localhost/app": {{Selector: "public.users.phone", FunctionName: "numerify", FunctionParams: []string{"###-####"}}},
	}}
	if err := writeMappingCache(path, legacy); err != nil {
		t.Fatal(err)
	}
	got, found, err := loadCachedMappings("postgres://localhost:5432/app")
	if err != nil || !found || got["public.users.phone"] != "numerify;###-####" {
		t.Fatalf("legacy cache = %#v, %v, %v", got, found, err)
	}
}

func TestSavingMappingsReplacesStaleEditorEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	const dsn = "postgres://localhost:5432/app"
	if err := saveCachedEntries(dsn, []tuiFakeDataEntry{{Selector: "public.users.email", FunctionName: "email"}}); err != nil {
		t.Fatal(err)
	}
	if err := saveCachedMappings(dsn, map[string]string{"name": "name"}); err != nil {
		t.Fatal(err)
	}
	got, found, err := loadCachedMappings(dsn)
	if err != nil || !found || len(got) != 1 || got["name"] != "name" {
		t.Fatalf("updated cache = %#v, %v, %v", got, found, err)
	}
	path, err := cachePath()
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := readMappingCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := persisted.Sources[pgDSNCacheKey(dsn)]; stale {
		t.Fatal("stale editor entries were retained")
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != cacheFileName {
		t.Fatalf("temporary cache files were left behind: %v", files)
	}
}

func TestSavingMappingsDoesNotOverwriteInvalidCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	path, err := cachePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	const invalid = "mappings: ["
	if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveCachedMappings("postgres://localhost/app", map[string]string{"email": "email"}); err == nil {
		t.Fatal("expected invalid cache error")
	}
	contents, err := os.ReadFile(path) //nolint:gosec // path is confined to t.TempDir
	if err != nil || string(contents) != invalid {
		t.Fatalf("invalid cache was overwritten: %q, %v", contents, err)
	}
}
