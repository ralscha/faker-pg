package faker

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBuildUpdateSQLQuotesIdentifiers(t *testing.T) {
	table := tableMeta{Schema: `odd"schema`, Name: "user data"}
	columns := []columnMeta{{Name: `full"name`}, {Name: "email"}}
	primaryKey := []string{"tenant id", "id"}

	got := buildUpdateSQL(table, columns, primaryKey)
	want := `UPDATE "odd""schema"."user data" SET "full""name" = $1, "email" = $2 WHERE "tenant id" = $3 AND "id" = $4`
	if got != want {
		t.Fatalf("buildUpdateSQL() = %q, want %q", got, want)
	}
}

func TestMatchingAnonymizationJobs(t *testing.T) {
	dataFaker, err := newDataFaker(map[string]string{"email": "email"})
	if err != nil {
		t.Fatalf("newDataFaker: %v", err)
	}
	tables := []tableMeta{
		{
			Schema:     "public",
			Name:       "users",
			PrimaryKey: &keyConstraint{Columns: []string{"id"}},
			Columns: []columnMeta{
				{Name: "id", Copyable: false},
				{Name: "email", DataType: "text", UDTName: "text", Copyable: true},
			},
		},
		{
			Schema:     "public",
			Name:       "audit",
			PrimaryKey: &keyConstraint{Columns: []string{"id"}},
			Columns:    []columnMeta{{Name: "message", Copyable: true}},
		},
	}

	jobs, err := matchingAnonymizationJobs(tables, dataFaker)
	if err != nil {
		t.Fatalf("matchingAnonymizationJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}
	if jobs[0].table.Name != "users" || len(jobs[0].columns) != 1 || jobs[0].columns[0].Name != "email" {
		t.Fatalf("unexpected job: %#v", jobs[0])
	}
}

func TestMatchingAnonymizationJobsRejectsIncompatibleRule(t *testing.T) {
	dataFaker, err := newDataFaker(map[string]string{"age": "email"})
	if err != nil {
		t.Fatalf("newDataFaker: %v", err)
	}
	tables := []tableMeta{{
		Schema:     "public",
		Name:       "users",
		PrimaryKey: &keyConstraint{Columns: []string{"id"}},
		Columns:    []columnMeta{{Name: "age", DataType: "integer", UDTName: "int4", Copyable: true}},
	}}

	if _, err := matchingAnonymizationJobs(tables, dataFaker); err == nil {
		t.Fatal("incompatible rule unexpectedly succeeded")
	}
}

func TestSummarizeAnonymizationResultsIsDeterministic(t *testing.T) {
	results := []anonymizationTableResult{
		{job: anonymizationJob{index: 1, table: tableMeta{Schema: "public", Name: "z"}, columns: []columnMeta{{Name: "value"}}}, rows: 2},
		{job: anonymizationJob{index: 0, table: tableMeta{Schema: "public", Name: "a"}, columns: []columnMeta{{Name: "value"}}}, rows: 3},
	}

	summary := summarizeAnonymizationResults(results, true, 25)
	if summary.err != nil {
		t.Fatalf("summarize: %v", summary.err)
	}
	if summary.tableCount != 2 || summary.rowCount != 5 {
		t.Fatalf("counts = %d tables/%d rows, want 2/5", summary.tableCount, summary.rowCount)
	}
	if len(summary.logLines) != 2 || !strings.Contains(summary.logLines[0], `"a"`) || !strings.Contains(summary.logLines[1], `"z"`) {
		t.Fatalf("logs are not in job order: %#v", summary.logLines)
	}
}

func TestSummarizePrefersExecutionErrorOverCancellation(t *testing.T) {
	boom := errors.New("boom")
	results := []anonymizationTableResult{
		{job: anonymizationJob{index: 0, table: tableMeta{Schema: "public", Name: "a"}}, err: context.Canceled},
		{job: anonymizationJob{index: 1, table: tableMeta{Schema: "public", Name: "b"}}, err: boom},
	}

	summary := summarizeAnonymizationResults(results, false, 1)
	if !errors.Is(summary.err, boom) {
		t.Fatalf("error = %v, want wrapped boom", summary.err)
	}
}
