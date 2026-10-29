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
    t.table_name,
    c.relkind = 'p'
FROM information_schema.tables t
JOIN pg_catalog.pg_namespace n ON n.nspname = t.table_schema
JOIN pg_catalog.pg_class c ON c.relnamespace = n.oid AND c.relname = t.table_name
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
		var partitioned bool
		if err := rows.Scan(&table.Schema, &table.Name, &partitioned); err != nil {
			return nil, fmt.Errorf("scan table: %w", err)
		}
		if !shouldIncludeTable(table.Schema, table.Name, iSchemas, eSchemas, iTables, eTables) {
			continue
		}
		// Partitioned roots have no storage. Processing their leaf tables
		// separately makes table filters and per-column rules unambiguous.
		if partitioned {
			if len(iTables) > 0 {
				return nil, fmt.Errorf("%s is a partitioned table; include its leaf partitions instead", table.FQTN())
			}
			continue
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tables: %w", err)
	}
	rows.Close()

	// Finish consuming the table query before issuing metadata queries. This is
	// important when the caller deliberately uses a one-connection pool.
	for i := range tables {
		table := &tables[i]
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

		referenced, err := loadReferencedColumns(ctx, db, table.Schema, table.Name)
		if err != nil {
			return nil, fmt.Errorf("load referenced columns for %s.%s: %w", table.Schema, table.Name, err)
		}
		table.Referenced = referenced
		markUnsafeColumns(table)
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
    c.ordinal_position,
    CASE WHEN c.is_generated <> 'NEVER' OR c.is_identity = 'YES' THEN true ELSE false END
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
			&col.Generated,
		); err != nil {
			return nil, err
		}
		col.UDTName = strings.ToLower(col.UDTName)
		col.DataType = strings.ToLower(col.DataType)

		switch {
		case col.Generated:
			col.SkipReason = "generated and identity columns are not anonymized"
		case col.IsArray:
			col.Copyable = false
			col.SkipReason = "array types are not supported"
		case supportedColumnType(col.UDTName):
			col.Copyable = true
		default:
			col.SkipReason = "unsupported type: " + col.UDTName
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
    c.conname,
    array_agg(a.attname ORDER BY k.ordinality)
FROM pg_catalog.pg_constraint c
CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY k(attnum, ordinality)
JOIN pg_catalog.pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
WHERE c.contype = 'p' AND c.conrelid = $1::pg_catalog.regclass
GROUP BY c.oid, c.conname;`

	var pk keyConstraint
	err := db.QueryRow(ctx, pkSQL, pgx.Identifier{schema, table}.Sanitize()).Scan(&pk.Name, &pk.Columns)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &pk, nil
}

func loadForeignKeys(ctx context.Context, db *pgxpool.Pool, schema, table string) ([]foreignKey, error) {
	// PostgreSQL permits duplicate constraint names across tables, and foreign
	// keys may reference a standalone unique index. Catalog OIDs cover both
	// cases without ambiguous information_schema constraint-name joins.
	const fkSQL = `
SELECT
    c.conname,
    ARRAY(SELECT a.attname
          FROM unnest(c.conkey) WITH ORDINALITY k(attnum, ordinality)
          JOIN pg_catalog.pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
          ORDER BY k.ordinality),
    n.nspname,
    r.relname,
    ARRAY(SELECT a.attname
          FROM unnest(c.confkey) WITH ORDINALITY k(attnum, ordinality)
          JOIN pg_catalog.pg_attribute a ON a.attrelid = c.confrelid AND a.attnum = k.attnum
          ORDER BY k.ordinality),
    CASE c.confdeltype WHEN 'a' THEN 'NO ACTION' WHEN 'r' THEN 'RESTRICT'
         WHEN 'c' THEN 'CASCADE' WHEN 'n' THEN 'SET NULL' WHEN 'd' THEN 'SET DEFAULT' END,
    CASE c.confupdtype WHEN 'a' THEN 'NO ACTION' WHEN 'r' THEN 'RESTRICT'
         WHEN 'c' THEN 'CASCADE' WHEN 'n' THEN 'SET NULL' WHEN 'd' THEN 'SET DEFAULT' END
FROM pg_catalog.pg_constraint c
JOIN pg_catalog.pg_class r ON r.oid = c.confrelid
JOIN pg_catalog.pg_namespace n ON n.oid = r.relnamespace
WHERE c.contype = 'f' AND c.conrelid = $1::pg_catalog.regclass
ORDER BY c.conname, c.oid;`

	rows, err := db.Query(ctx, fkSQL, pgx.Identifier{schema, table}.Sanitize())
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

func loadReferencedColumns(ctx context.Context, db *pgxpool.Pool, schema, table string) ([]string, error) {
	const referencedSQL = `
SELECT DISTINCT a.attname
FROM pg_catalog.pg_constraint c
JOIN pg_catalog.pg_attribute a ON a.attrelid = c.confrelid AND a.attnum = ANY(c.confkey)
WHERE c.contype = 'f' AND c.confrelid = $1::pg_catalog.regclass
ORDER BY a.attname;`

	rows, err := db.Query(ctx, referencedSQL, pgx.Identifier{schema, table}.Sanitize())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func supportedColumnType(udtName string) bool {
	switch strings.ToLower(udtName) {
	case "varchar", "bpchar", "text", dtName, "citext",
		"int2", "int4", dtInt8, "float4", "float8", "numeric", "money",
		"bool", "date", "timestamp", "timestamptz", "time", "timetz",
		dtUUID, "inet", "cidr", "macaddr", "macaddr8":
		return true
	default:
		return false
	}
}

func markUnsafeColumns(table *tableMeta) {
	if table == nil {
		return
	}
	if table.PrimaryKey == nil || len(table.PrimaryKey.Columns) == 0 {
		for i := range table.Columns {
			table.Columns[i].Copyable = false
			table.Columns[i].SkipReason = "tables without a primary key are not anonymized"
		}
		return
	}

	keyColumns := make(map[string]string, len(table.PrimaryKey.Columns))
	for _, column := range table.PrimaryKey.Columns {
		keyColumns[column] = "primary key columns are not anonymized"
	}
	for _, fk := range table.ForeignKeys {
		for _, column := range fk.Columns {
			if _, isPrimaryKey := keyColumns[column]; !isPrimaryKey {
				keyColumns[column] = "foreign key columns are not anonymized"
			}
		}
	}
	for _, column := range table.Referenced {
		if _, isPrimaryKey := keyColumns[column]; !isPrimaryKey {
			keyColumns[column] = "columns referenced by foreign keys are not anonymized"
		}
	}
	for i := range table.Columns {
		if reason, unsafe := keyColumns[table.Columns[i].Name]; unsafe {
			table.Columns[i].Copyable = false
			table.Columns[i].SkipReason = reason
		}
	}
}
