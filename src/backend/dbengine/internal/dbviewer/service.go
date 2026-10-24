package dbviewer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/hakamavicena/if3110-dbengine/internal/apperr"
	"github.com/hakamavicena/if3110-dbengine/internal/dbtarget"
	"github.com/hakamavicena/if3110-dbengine/internal/ports"
	"github.com/jackc/pgx/v5"
)

const (
	defaultPageSize = 25
	maxPageSize     = 200
	viewTimeout     = "5s"
)

type Service struct {
	authz ports.Authorizer
	pools ports.PoolProvider
}

func NewService(authz ports.Authorizer, pools ports.PoolProvider) *Service {
	return &Service{authz: authz, pools: pools}
}

func (s *Service) authorize(ctx context.Context, userID, dbID int64) error {
	if s.authz == nil {
		return fmt.Errorf("viewer authorizer is not configured")
	}
	ok, err := s.authz.CanView(ctx, userID, dbID)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.ErrForbidden
	}
	return nil
}

func (s *Service) withReadTx(ctx context.Context, userID, dbID int64, fn func(pgx.Tx) error) error {
	if err := s.authorize(ctx, userID, dbID); err != nil {
		return err
	}
	pool, err := s.pools.Pool(ctx, dbID)
	if err != nil {
		return err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return dbtarget.ClassifyError(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '"+viewTimeout+"'"); err != nil {
		return dbtarget.ClassifyError(err)
	}
	return dbtarget.ClassifyError(fn(tx))
}

func (s *Service) ListSchemas(ctx context.Context, userID, dbID int64) ([]string, error) {
	var out []string
	err := s.withReadTx(ctx, userID, dbID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, qSchemas)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			out = append(out, name)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) ListTables(ctx context.Context, userID, dbID int64, schema string) ([]TableInfo, error) {
	var out []TableInfo
	err := s.withReadTx(ctx, userID, dbID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, qTables, schema)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var table TableInfo
			if err := rows.Scan(&table.Schema, &table.Name, &table.Type); err != nil {
				return err
			}
			out = append(out, table)
		}
		return rows.Err()
	})
	return out, err
}

func tableKind(ctx context.Context, tx pgx.Tx, schema, table string) (string, error) {
	var kind string
	err := tx.QueryRow(ctx, qTableKind, schema, table).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperr.ErrNotFound
	}
	return kind, err
}

func (s *Service) GetTableDetail(ctx context.Context, userID, dbID int64, schema, table string) (*TableDetail, error) {
	var detail TableDetail
	err := s.withReadTx(ctx, userID, dbID, func(tx pgx.Tx) error {
		kind, err := tableKind(ctx, tx, schema, table)
		if err != nil {
			return err
		}
		detail.Table = TableInfo{Schema: schema, Name: table, Type: kind}

		columnRows, err := tx.Query(ctx, qColumns, schema, table)
		if err != nil {
			return err
		}
		for columnRows.Next() {
			var column ColumnInfo
			if err := columnRows.Scan(&column.Position, &column.Name, &column.DataType, &column.Nullable, &column.Default); err != nil {
				columnRows.Close()
				return err
			}
			detail.Columns = append(detail.Columns, column)
		}
		columnRows.Close()
		if err := columnRows.Err(); err != nil {
			return err
		}

		constraintRows, err := tx.Query(ctx, qConstraints, schema, table)
		if err != nil {
			return err
		}
		for constraintRows.Next() {
			var constraint ConstraintInfo
			var kind string
			if err := constraintRows.Scan(&constraint.Name, &kind, &constraint.Definition); err != nil {
				constraintRows.Close()
				return err
			}
			constraint.Type = constraintLabel[kind]
			detail.Constraints = append(detail.Constraints, constraint)
		}
		constraintRows.Close()
		if err := constraintRows.Err(); err != nil {
			return err
		}

		indexRows, err := tx.Query(ctx, qIndexes, schema, table)
		if err != nil {
			return err
		}
		defer indexRows.Close()
		for indexRows.Next() {
			var index IndexInfo
			if err := indexRows.Scan(&index.Name, &index.Definition); err != nil {
				return err
			}
			index.Unique = strings.Contains(index.Definition, "UNIQUE INDEX")
			detail.Indexes = append(detail.Indexes, index)
		}
		return indexRows.Err()
	})
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

func normalizePage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	maxPage := math.MaxInt / size
	if page > maxPage {
		page = maxPage
	}
	return page, size
}

func (s *Service) GetTableData(ctx context.Context, userID, dbID int64, schema, table string, page, size int) (*TableData, error) {
	page, size = normalizePage(page, size)
	var out *TableData
	err := s.withReadTx(ctx, userID, dbID, func(tx pgx.Tx) error {
		kind, err := tableKind(ctx, tx, schema, table)
		if err != nil {
			return err
		}
		ident := (pgx.Identifier{schema, table}).Sanitize()
		order := ""
		if kind == "BASE TABLE" {
			order = " ORDER BY ctid"
		}
		var total int64
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+ident).Scan(&total); err != nil {
			return err
		}
		offset := (page - 1) * size
		query := fmt.Sprintf("SELECT * FROM %s%s LIMIT %d OFFSET %d", ident, order, size, offset)
		rows, err := tx.Query(ctx, query, pgx.QueryExecModeSimpleProtocol)
		if err != nil {
			return err
		}
		defer rows.Close()
		data := &TableData{Page: page, PageSize: size, Total: total}
		for _, field := range rows.FieldDescriptions() {
			data.Columns = append(data.Columns, field.Name)
		}
		for rows.Next() {
			raw := rows.RawValues()
			row := make([]*string, len(raw))
			for i, value := range raw {
				if value != nil {
					text := string(value)
					row[i] = &text
				}
			}
			data.Rows = append(data.Rows, row)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		data.Pages = int(total / int64(size))
		if total%int64(size) != 0 {
			data.Pages++
		}
		out = data
		return nil
	})
	return out, err
}
