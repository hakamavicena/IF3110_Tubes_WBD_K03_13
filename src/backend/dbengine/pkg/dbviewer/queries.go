package dbviewer

const (
	qSchemas = `
		SELECT schema_name FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog','information_schema')
		  AND schema_name NOT LIKE 'pg_toast%' AND schema_name NOT LIKE 'pg_temp%'
		ORDER BY schema_name`

	qTables = `
		SELECT table_schema, table_name, table_type
		FROM information_schema.tables
		WHERE table_schema = $1
		ORDER BY table_name`

	qTableKind = `
		SELECT table_type FROM information_schema.tables
		WHERE table_schema = $1 AND table_name = $2`

	qColumns = `
		SELECT ordinal_position, column_name, data_type, is_nullable = 'YES', column_default
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`

	qConstraints = `
		SELECT c.conname, c.contype::text, pg_get_constraintdef(c.oid)
		FROM pg_constraint c
		JOIN pg_class r ON r.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = r.relnamespace
		WHERE n.nspname = $1 AND r.relname = $2
		ORDER BY c.contype, c.conname`

	qIndexes = `
		SELECT indexname, indexdef
		FROM pg_indexes
		WHERE schemaname = $1 AND tablename = $2
		ORDER BY indexname`
)

var constraintLabel = map[string]string{
	"p": "PRIMARY KEY", "f": "FOREIGN KEY", "u": "UNIQUE", "c": "CHECK", "x": "EXCLUDE",
}
