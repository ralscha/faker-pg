package faker

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests use an isolated schema in an explicitly configured test database.
// A one-connection pool also detects metadata queries that leave rows open.
func integrationDatabase(t *testing.T) (context.Context, *pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("FAKER_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("set FAKER_PG_TEST_DSN to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid FAKER_PG_TEST_DSN")
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create test pool: %v", err)
	}
	schema := fmt.Sprintf("faker_pg_test_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		pool.Close()
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
		pool.Close()
	})
	return ctx, pool, schema
}

func integrationSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema, sql string) {
	t.Helper()
	sql = strings.ReplaceAll(sql, "{{schema}}", pgx.Identifier{schema}.Sanitize())
	if _, err := pool.Exec(ctx, sql); err != nil {
		t.Fatalf("prepare test data: %v", err)
	}
}

func TestPostgresForeignKeyDiscovery(t *testing.T) {
	ctx, pool, schema := integrationDatabase(t)
	integrationSQL(t, ctx, pool, schema, `
CREATE TABLE {{schema}}.parent_a (id int PRIMARY KEY, code text UNIQUE, email text);
CREATE TABLE {{schema}}.parent_b (id int PRIMARY KEY, code text UNIQUE);
CREATE TABLE {{schema}}.child_a (id int PRIMARY KEY, code text,
    CONSTRAINT repeated_name FOREIGN KEY (code) REFERENCES {{schema}}.parent_a (code) ON UPDATE CASCADE);
CREATE TABLE {{schema}}.child_b (id int PRIMARY KEY, code text,
    CONSTRAINT repeated_name FOREIGN KEY (code) REFERENCES {{schema}}.parent_b (code) ON DELETE CASCADE);
CREATE TABLE {{schema}}.indexed_parent (id int PRIMARY KEY, a text, b text, email text);
CREATE UNIQUE INDEX indexed_parent_key ON {{schema}}.indexed_parent (a, b);
CREATE TABLE {{schema}}.indexed_child (id int PRIMARY KEY, x text, y text,
    FOREIGN KEY (x, y) REFERENCES {{schema}}.indexed_parent (b, a));`)

	fks, err := loadForeignKeys(ctx, pool, schema, "child_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(fks) != 1 || fks[0].RefTable != "parent_a" || fks[0].UpdateAction != "CASCADE" || fks[0].DeleteAction != "NO ACTION" {
		t.Fatalf("same-named constraints were confused: %#v", fks)
	}
	fks, err = loadForeignKeys(ctx, pool, schema, "indexed_child")
	if err != nil {
		t.Fatal(err)
	}
	if len(fks) != 1 || !slices.Equal(fks[0].Columns, []string{"x", "y"}) || !slices.Equal(fks[0].RefColumns, []string{"b", "a"}) {
		t.Fatalf("standalone unique index reference or column order lost: %#v", fks)
	}
	tables, err := loadTables(ctx, pool, []string{schema}, nil, []string{"indexed_parent", "indexed_child"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 2 {
		t.Fatalf("loaded %d tables, want both sides of the foreign key", len(tables))
	}
	for _, table := range tables {
		for _, column := range table.Columns {
			if column.Name == "email" {
				if !column.Copyable {
					t.Errorf("ordinary email column excluded: %#v", column)
				}
			} else if column.Copyable {
				t.Errorf("key column left anonymizable: %s.%s", table.Name, column.Name)
			}
		}
	}
}

func TestPostgresInheritanceRespectsTableFilters(t *testing.T) {
	ctx, pool, schema := integrationDatabase(t)
	integrationSQL(t, ctx, pool, schema, `
CREATE TABLE {{schema}}.parent (id int PRIMARY KEY, email text);
CREATE TABLE {{schema}}.child (PRIMARY KEY (id)) INHERITS ({{schema}}.parent);
INSERT INTO {{schema}}.parent VALUES (1, 'parent-original');
INSERT INTO {{schema}}.child VALUES (1, 'child-original'), (2, 'child-original');`)

	tables, err := loadTables(ctx, pool, []string{schema}, nil, nil, []string{"child"})
	if err != nil || len(tables) != 1 {
		t.Fatalf("load tables: %v (%d tables)", err, len(tables))
	}
	faker, err := newDataFaker(map[string]string{"email": "email"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := anonymizeTable(ctx, pool, tables[0], tables[0].CopyColumns(), faker, 1)
	if err != nil || rows != 1 {
		t.Fatalf("anonymize parent: %v (%d rows)", err, rows)
	}
	var changed int
	query := "SELECT count(*) FROM ONLY " + pgx.Identifier{schema, "parent"}.Sanitize() + " WHERE email <> 'parent-original'"
	if err := pool.QueryRow(ctx, query).Scan(&changed); err != nil || changed != 1 {
		t.Fatalf("parent update: %v (%d changed)", err, changed)
	}
	query = "SELECT count(*) FROM ONLY " + pgx.Identifier{schema, "child"}.Sanitize() + " WHERE email <> 'child-original'"
	if err := pool.QueryRow(ctx, query).Scan(&changed); err != nil || changed != 0 {
		t.Fatalf("excluded child changed: %v (%d changed)", err, changed)
	}
}

func TestPostgresPartitionLeavesAreProcessedOnce(t *testing.T) {
	ctx, pool, schema := integrationDatabase(t)
	integrationSQL(t, ctx, pool, schema, `
CREATE TABLE {{schema}}.events (id int PRIMARY KEY, email text) PARTITION BY RANGE (id);
CREATE TABLE {{schema}}.events_a PARTITION OF {{schema}}.events FOR VALUES FROM (0) TO (100);
CREATE TABLE {{schema}}.events_b PARTITION OF {{schema}}.events FOR VALUES FROM (100) TO (200);
INSERT INTO {{schema}}.events VALUES (1, 'original'), (101, 'original');`)

	if _, err := loadTables(ctx, pool, []string{schema}, nil, []string{"events"}, nil); err == nil || !strings.Contains(err.Error(), "leaf partitions") {
		t.Fatalf("partition-root selection should explain leaf selection: %v", err)
	}
	tables, err := loadTables(ctx, pool, []string{schema}, nil, nil, []string{"events_b"})
	if err != nil || len(tables) != 1 || tables[0].Name != "events_a" {
		t.Fatalf("load filtered partitions: %v (%#v)", err, tables)
	}
	faker, err := newDataFaker(map[string]string{"email": "email"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := anonymizeTable(ctx, pool, tables[0], tables[0].CopyColumns(), faker, 1)
	if err != nil || rows != 1 {
		t.Fatalf("anonymize partition: %v (%d rows)", err, rows)
	}
	var changed int
	query := "SELECT count(*) FROM " + pgx.Identifier{schema, "events_b"}.Sanitize() + " WHERE email <> 'original'"
	if err := pool.QueryRow(ctx, query).Scan(&changed); err != nil || changed != 0 {
		t.Fatalf("excluded partition changed: %v (%d changed)", err, changed)
	}
	tables, err = loadTables(ctx, pool, []string{schema}, nil, nil, nil)
	if err != nil || len(tables) != 2 {
		t.Fatalf("load partition leaves: %v (%d tables)", err, len(tables))
	}
	jobs, err := matchingAnonymizationJobs(tables, faker)
	if err != nil {
		t.Fatal(err)
	}
	summary := summarizeAnonymizationResults(executeAnonymizationJobs(ctx, pool, faker, jobs, 1, 2), false, 1)
	if summary.err != nil || summary.rowCount != 2 || summary.tableCount != 2 {
		t.Fatalf("partitions should each be processed once: %#v", summary)
	}
}

func TestPostgresTableFailureRollsBackEarlierBatches(t *testing.T) {
	ctx, pool, schema := integrationDatabase(t)
	integrationSQL(t, ctx, pool, schema, `
CREATE TABLE {{schema}}.users (id int PRIMARY KEY, email text CHECK (id <> 2 OR email = 'original'));
INSERT INTO {{schema}}.users VALUES (1, 'original'), (2, 'original');`)
	tables, err := loadTables(ctx, pool, []string{schema}, nil, nil, nil)
	if err != nil || len(tables) != 1 {
		t.Fatalf("load tables: %v (%d tables)", err, len(tables))
	}
	faker, err := newDataFaker(map[string]string{"email": "email"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := anonymizeTable(ctx, pool, tables[0], tables[0].CopyColumns(), faker, 1); err == nil {
		t.Fatal("expected the second batch to violate the check constraint")
	}
	var changed int
	query := "SELECT count(*) FROM " + tables[0].FQTN() + " WHERE email <> 'original'"
	if err := pool.QueryRow(ctx, query).Scan(&changed); err != nil || changed != 0 {
		t.Fatalf("failed table retained changes from an earlier batch: %v (%d changed)", err, changed)
	}
}
