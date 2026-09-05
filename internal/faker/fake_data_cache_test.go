package faker

import "testing"

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
