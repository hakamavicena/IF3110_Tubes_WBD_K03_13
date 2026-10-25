package dbviewer

type TableInfo struct {
	Schema string
	Name   string
	Type   string
}

type ColumnInfo struct {
	Position int
	Name     string
	DataType string
	Nullable bool
	Default  *string
}

type ConstraintInfo struct {
	Name       string
	Type       string
	Definition string
}

type IndexInfo struct {
	Name       string
	Definition string
	Unique     bool
}

type TableDetail struct {
	Table       TableInfo
	Columns     []ColumnInfo
	Constraints []ConstraintInfo
	Indexes     []IndexInfo
}

type TableData struct {
	Columns  []string
	Rows     [][]*string
	Page     int
	PageSize int
	Total    int64
	Pages    int
}
