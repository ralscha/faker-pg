package faker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func loadTables(ctx context.Context, db *pgxpool.Pool, includeSchemas, excludeSchemas, includeTables, excludeTables []string) ([]tableMeta, error) {
	const tablesSQL = `
SELECT
    t.table_schema,
    t.table_name
FROM information_schema.tables t
WHERE t.table_type = 'BASE TABLE'
  AND t.table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY t.table_schema, t.table_name;`

	rows, err := db.Query(ctx, tablesSQL)
	if err != nil {
		return nil, fmt.Errorf("query tables: %w", err)
	}
	defer rows.Close()

	iSchemas := makeStringSet(includeSchemas)
	eSchemas := makeStringSet(excludeSchemas)
	iTables := makeStringSet(includeTables)
	eTables := makeStringSet(excludeTables)

	var tables []tableMeta
	for rows.Next() {
		var table tableMeta
		if err := rows.Scan(&table.Schema, &table.Name); err != nil {
			return nil, fmt.Errorf("scan table: %w", err)
		}
		if !shouldIncludeTable(table.Schema, table.Name, iSchemas, eSchemas, iTables, eTables) {
			continue
		}
		columns, err := loadColumns(ctx, db, table.Schema, table.Name)
		if err != nil {
			return nil, fmt.Errorf("load columns for %s.%s: %w", table.Schema, table.Name, err)
		}
		table.Columns = columns

		pk, err := loadPrimaryKey(ctx, db, table.Schema, table.Name)
		if err != nil {
			return nil, fmt.Errorf("load primary key for %s.%s: %w", table.Schema, table.Name, err)
		}
		table.PrimaryKey = pk

		fks, err := loadForeignKeys(ctx, db, table.Schema, table.Name)
		if err != nil {
			return nil, fmt.Errorf("load foreign keys for %s.%s: %w", table.Schema, table.Name, err)
		}
		table.ForeignKeys = fks

		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tables: %w", err)
	}
	return tables, nil
}

func shouldIncludeTable(schema, name string, includeSchemas, excludeSchemas, includeTables, excludeTables map[string]struct{}) bool {
	s := normalizeFilterName(schema)
	n := normalizeFilterName(name)
	full := s + "." + n

	if len(excludeSchemas) > 0 {
		if _, ok := excludeSchemas[s]; ok {
			return false
		}
	}
	if len(excludeTables) > 0 {
		_, byName := excludeTables[n]
		_, byFull := excludeTables[full]
		if byName || byFull {
			return false
		}
	}
	if len(includeSchemas) > 0 {
		if _, ok := includeSchemas[s]; !ok {
			return false
		}
	}
	if len(includeTables) > 0 {
		_, byName := includeTables[n]
		_, byFull := includeTables[full]
		if !byName && !byFull {
			return false
		}
	}
	return true
}

func loadColumns(ctx context.Context, db *pgxpool.Pool, schema, table string) ([]columnMeta, error) {
	const columnsSQL = `
SELECT
    c.column_name,
    c.data_type,
    c.udt_name,
    COALESCE(c.character_maximum_length, -1),
    COALESCE(c.numeric_precision, 0),
    COALESCE(c.numeric_scale, 0),
    CASE WHEN c.is_nullable = 'YES' THEN true ELSE false END,
    CASE WHEN c.data_type = 'ARRAY' THEN true ELSE false END,
    c.ordinal_position
FROM information_schema.columns c
WHERE c.table_schema = $1 AND c.table_name = $2
ORDER BY c.ordinal_position;`

	rows, err := db.Query(ctx, columnsSQL, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []columnMeta
	for rows.Next() {
		var col columnMeta
		if err := rows.Scan(
			&col.Name,
			&col.DataType,
			&col.UDTName,
			&col.MaxLength,
			&col.Precision,
			&col.Scale,
			&col.Nullable,
			&col.IsArray,
			&col.OrdinalPos,
		); err != nil {
			return nil, err
		}
		col.UDTName = strings.ToLower(col.UDTName)
		col.DataType = strings.ToLower(col.DataType)

		switch col.UDTName {
		case "bytea", "json", "jsonb", "xml", "pg_lsn", "txid_snapshot":
			col.Copyable = false
			col.SkipReason = "unsupported type: " + col.UDTName
		default:
			col.Copyable = true
		}
		if col.IsArray {
			col.Copyable = false
			col.SkipReason = "array types are not supported"
		}

		columns = append(columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func loadPrimaryKey(ctx context.Context, db *pgxpool.Pool, schema, table string) (*keyConstraint, error) {
	const pkSQL = `
SELECT
    tc.constraint_name,
    array_agg(kcu.column_name ORDER BY kcu.ordinal_position)
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
    ON tc.constraint_name = kcu.constraint_name
    AND tc.table_schema = kcu.table_schema
WHERE tc.constraint_type = 'PRIMARY KEY'
    AND tc.table_schema = $1
    AND tc.table_name = $2
GROUP BY tc.constraint_name;`

	var pk keyConstraint
	err := db.QueryRow(ctx, pkSQL, schema, table).Scan(&pk.Name, &pk.Columns)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &pk, nil
}

func loadForeignKeys(ctx context.Context, db *pgxpool.Pool, schema, table string) ([]foreignKey, error) {
	const fkSQL = `
SELECT
    tc.constraint_name,
    array_agg(kcu.column_name ORDER BY kcu.ordinal_position),
    ccu.table_schema,
    ccu.table_name,
    array_agg(ccu.column_name ORDER BY kcu.ordinal_position),
    rc.delete_rule,
    rc.update_rule
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
    ON tc.constraint_name = kcu.constraint_name
    AND tc.table_schema = kcu.table_schema
JOIN information_schema.constraint_column_usage ccu
    ON tc.constraint_name = ccu.constraint_name
    AND tc.table_schema = ccu.table_schema
JOIN information_schema.referential_constraints rc
    ON tc.constraint_name = rc.constraint_name
    AND tc.table_schema = rc.constraint_schema
WHERE tc.constraint_type = 'FOREIGN KEY'
    AND tc.table_schema = $1
    AND tc.table_name = $2
GROUP BY tc.constraint_name, ccu.table_schema, ccu.table_name, rc.delete_rule, rc.update_rule;`

	rows, err := db.Query(ctx, fkSQL, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fks []foreignKey
	for rows.Next() {
		var fk foreignKey
		if err := rows.Scan(
			&fk.Name,
			&fk.Columns,
			&fk.RefSchema,
			&fk.RefTable,
			&fk.RefColumns,
			&fk.DeleteAction,
			&fk.UpdateAction,
		); err != nil {
			return nil, err
		}
		fks = append(fks, fk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return fks, nil
}
