package faker

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type anonymizationResult struct {
	tableCount int
	rowCount   int64
	logLines   []string
	err        error
}

type anonymizationJob struct {
	index   int
	table   tableMeta
	columns []columnMeta
}

type anonymizationTableResult struct {
	job  anonymizationJob
	rows int64
	err  error
}

func runAnonymization(ctx context.Context, cfg config) anonymizationResult {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return anonymizationResult{err: fmt.Errorf("parse DSN: %w", err)}
	}
	workers := max(1, cfg.Workers)
	poolCfg.MaxConns = int32(min(workers, math.MaxInt32))
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return anonymizationResult{err: fmt.Errorf("connect: %w", err)}
	}
	defer pool.Close()

	tables, err := loadTables(ctx, pool, cfg.IncludeSchemas, cfg.ExcludeSchemas, cfg.IncludeTables, cfg.ExcludeTables)
	if err != nil {
		return anonymizationResult{err: err}
	}

	dataFaker, err := newDataFaker(cfg.FakeData)
	if err != nil {
		return anonymizationResult{err: fmt.Errorf("init faker: %w", err)}
	}
	if dataFaker == nil {
		return anonymizationResult{err: fmt.Errorf("no fake data rules configured")}
	}

	jobs, err := matchingAnonymizationJobs(tables, dataFaker)
	if err != nil {
		return anonymizationResult{err: err}
	}
	if len(jobs) == 0 {
		return anonymizationResult{err: fmt.Errorf("no configured rules match anonymizable columns")}
	}

	batchSize := max(1, cfg.BatchSize)
	results := executeAnonymizationJobs(ctx, pool, dataFaker, jobs, batchSize, workers)
	summary := summarizeAnonymizationResults(results, cfg.Verbose, batchSize)
	if summary.err == nil && len(results) != len(jobs) {
		if ctx.Err() != nil {
			summary.err = fmt.Errorf("anonymization interrupted: %w", ctx.Err())
		} else {
			summary.err = fmt.Errorf("anonymization stopped before all tables completed")
		}
	}
	if summary.err != nil {
		return summary
	}

	if err := saveCachedMappings(cfg.DSN, cfg.FakeData); err != nil {
		summary.logLines = append(summary.logLines, fmt.Sprintf("warning: failed to save cache: %v", err))
	}
	return summary
}

func matchingAnonymizationJobs(tables []tableMeta, dataFaker *dataFaker) ([]anonymizationJob, error) {
	var jobs []anonymizationJob
	for _, table := range tables {
		var columns []columnMeta
		for _, column := range table.CopyColumns() {
			rule, ok := dataFaker.matchRule(table, column)
			if !ok {
				continue
			}
			if !fakeFunctionCompatible(fakerTypeName(column), rule.lookupName, rule.info.Output) {
				return nil, fmt.Errorf(
					"fake-data rule %q produces %s, which is incompatible with %s.%s (%s)",
					rule.selector, rule.info.Output, table.FQTN(), column.Name, fakerTypeName(column),
				)
			}
			columns = append(columns, column)
		}
		if len(columns) > 0 {
			jobs = append(jobs, anonymizationJob{index: len(jobs), table: table, columns: columns})
		}
	}
	return jobs, nil
}

func executeAnonymizationJobs(
	ctx context.Context,
	pool *pgxpool.Pool,
	dataFaker *dataFaker,
	jobs []anonymizationJob,
	batchSize int,
	workers int,
) []anonymizationTableResult {
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobQueue := make(chan anonymizationJob, len(jobs))
	resultQueue := make(chan anonymizationTableResult, len(jobs))
	for _, job := range jobs {
		jobQueue <- job
	}
	close(jobQueue)

	var wg sync.WaitGroup
	for range min(max(1, workers), len(jobs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobQueue {
				if workerCtx.Err() != nil {
					return
				}
				rows, err := anonymizeTable(workerCtx, pool, job.table, job.columns, dataFaker, batchSize)
				resultQueue <- anonymizationTableResult{job: job, rows: rows, err: err}
				if err != nil {
					cancel()
					return
				}
			}
		}()
	}

	wg.Wait()
	close(resultQueue)
	results := make([]anonymizationTableResult, 0, len(jobs))
	for result := range resultQueue {
		results = append(results, result)
	}
	return results
}

func summarizeAnonymizationResults(results []anonymizationTableResult, verbose bool, batchSize int) anonymizationResult {
	resultSlots := 0
	for i := range results {
		resultSlots = max(resultSlots, results[i].job.index+1)
	}
	ordered := make([]*anonymizationTableResult, resultSlots)
	for i := range results {
		result := &results[i]
		if result.job.index >= 0 && result.job.index < len(ordered) {
			ordered[result.job.index] = result
		}
	}

	var summary anonymizationResult
	var firstCancellation error
	for _, result := range ordered {
		if result == nil {
			continue
		}
		if result.err != nil {
			wrapped := fmt.Errorf("anonymize %s: %w", result.job.table.FQTN(), result.err)
			if !errors.Is(result.err, context.Canceled) && summary.err == nil {
				summary.err = wrapped
			}
			if firstCancellation == nil {
				firstCancellation = wrapped
			}
			continue
		}
		summary.tableCount++
		summary.rowCount += result.rows
		line := fmt.Sprintf("anonymized %s: %d rows", result.job.table.FQTN(), result.rows)
		if verbose {
			line += fmt.Sprintf(" (%d columns, batches of %d)", len(result.job.columns), batchSize)
		}
		summary.logLines = append(summary.logLines, line)
	}
	if summary.err == nil {
		summary.err = firstCancellation
	}
	return summary
}

func anonymizeTable(
	ctx context.Context,
	pool *pgxpool.Pool,
	table tableMeta,
	fakedColumns []columnMeta,
	dataFaker *dataFaker,
	batchSize int,
) (totalRows int64, err error) {
	primaryKeyColumns := table.PrimaryKeyColumns()
	if len(primaryKeyColumns) == 0 {
		return 0, fmt.Errorf("table has no primary key")
	}
	if len(fakedColumns) == 0 {
		return 0, nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(context.Background())
		}
	}()

	selectColumns := make([]string, len(primaryKeyColumns))
	for i, column := range primaryKeyColumns {
		selectColumns[i] = pgx.Identifier{column}.Sanitize()
	}
	const cursorName = "faker_pg_rows"
	declareSQL := fmt.Sprintf(
		"DECLARE %s NO SCROLL CURSOR FOR SELECT %s FROM %s ORDER BY %s",
		pgx.Identifier{cursorName}.Sanitize(),
		strings.Join(selectColumns, ", "),
		table.FQTN(),
		strings.Join(selectColumns, ", "),
	)
	if _, err = tx.Exec(ctx, declareSQL); err != nil {
		return 0, fmt.Errorf("declare row cursor: %w", err)
	}

	updateSQL := buildUpdateSQL(table, fakedColumns, primaryKeyColumns)
	valueFaker := gofakeit.New(0)
	for {
		primaryKeys, fetchErr := fetchPrimaryKeyBatch(ctx, tx, cursorName, len(primaryKeyColumns), max(1, batchSize))
		if fetchErr != nil {
			return totalRows, fetchErr
		}
		if len(primaryKeys) == 0 {
			break
		}

		batch := &pgx.Batch{}
		for _, primaryKey := range primaryKeys {
			args := make([]any, 0, len(fakedColumns)+len(primaryKey))
			for _, column := range fakedColumns {
				fakeValue, matched, fakeErr := dataFaker.fakeValue(valueFaker, table, column)
				if fakeErr != nil {
					return totalRows, fmt.Errorf("generate fake for %s: %w", column.Name, fakeErr)
				}
				if !matched {
					return totalRows, fmt.Errorf("faker rule for %s disappeared during execution", column.Name)
				}
				args = append(args, replaceValue(column, fakeValue))
			}
			args = append(args, primaryKey...)
			batch.Queue(updateSQL, args...)
		}

		batchResults := tx.SendBatch(ctx, batch)
		for range primaryKeys {
			tag, execErr := batchResults.Exec()
			if execErr != nil {
				_ = batchResults.Close()
				return totalRows, fmt.Errorf("update row: %w", execErr)
			}
			if tag.RowsAffected() != 1 {
				_ = batchResults.Close()
				return totalRows, fmt.Errorf("update row affected %d rows, expected 1", tag.RowsAffected())
			}
			totalRows++
		}
		if closeErr := batchResults.Close(); closeErr != nil {
			return totalRows, fmt.Errorf("finish update batch: %w", closeErr)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return totalRows, fmt.Errorf("commit transaction: %w", err)
	}
	return totalRows, nil
}

func fetchPrimaryKeyBatch(ctx context.Context, tx pgx.Tx, cursorName string, columnCount, batchSize int) ([][]any, error) {
	query := fmt.Sprintf(
		"FETCH FORWARD %d FROM %s",
		max(1, batchSize),
		pgx.Identifier{cursorName}.Sanitize(),
	)
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("fetch rows: %w", err)
	}
	defer rows.Close()

	var primaryKeys [][]any
	for rows.Next() {
		values := make([]any, columnCount)
		targets := make([]any, columnCount)
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, fmt.Errorf("scan primary key: %w", err)
		}
		primaryKeys = append(primaryKeys, values)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate primary keys: %w", err)
	}
	return primaryKeys, nil
}

func buildUpdateSQL(table tableMeta, fakedColumns []columnMeta, primaryKeyColumns []string) string {
	setClauses := make([]string, len(fakedColumns))
	for i, column := range fakedColumns {
		setClauses[i] = fmt.Sprintf("%s = $%d", pgx.Identifier{column.Name}.Sanitize(), i+1)
	}
	whereClauses := make([]string, len(primaryKeyColumns))
	for i, column := range primaryKeyColumns {
		whereClauses[i] = fmt.Sprintf("%s = $%d", pgx.Identifier{column}.Sanitize(), len(fakedColumns)+i+1)
	}
	return fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s",
		table.FQTN(),
		strings.Join(setClauses, ", "),
		strings.Join(whereClauses, " AND "),
	)
}
