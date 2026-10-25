package migration

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/hakamavicena/if3110-dbengine/internal/apperr"
)

const maxSQLBytes = 1 << 20

var txControl = regexp.MustCompile(`(?im)^\s*(commit|rollback|abort|start\s+transaction)\b`)

func ValidateInput(title, sql string) error {
	title = strings.TrimSpace(title)
	switch {
	case title == "" || utf8.RuneCountInString(title) > 200:
		return fmt.Errorf("%w: judul wajib diisi (maks 200 karakter)", apperr.ErrInvalidInput)
	case strings.TrimSpace(sql) == "":
		return fmt.Errorf("%w: SQL tidak boleh kosong", apperr.ErrInvalidInput)
	case len(sql) > maxSQLBytes:
		return fmt.Errorf("%w: SQL terlalu besar (maks 1 MiB)", apperr.ErrInvalidInput)
	case txControl.MatchString(sql):
		return fmt.Errorf("%w: jangan sertakan COMMIT/ROLLBACK; platform mengelola transaksi", apperr.ErrInvalidInput)
	default:
		return nil
	}
}
