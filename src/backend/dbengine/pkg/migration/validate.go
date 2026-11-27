package migration

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"backend/dbengine/pkg/apperr"
)

const maxSQLBytes = 1 << 20

var txControl = regexp.MustCompile(
	`(?im)(?:^|;)\s*(?:COMMIT|ROLLBACK|ABORT|BEGIN|END|SAVEPOINT|RELEASE)\b|(?:^|;)\s*(?:START|PREPARE)\s+TRANSACTION\b`,
)

func ValidateInput(title, sql string) error {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 200 {
		return fmt.Errorf("%w: judul wajib diisi (maks 200 karakter)", apperr.ErrInvalidInput)
	}
	return validateSQL(sql, "SQL")
}

func ValidateDefinition(
	title, sqlUp, sqlDown string,
	isIrreversible bool,
	irreversibleReason string,
) error {
	if err := ValidateInput(title, sqlUp); err != nil {
		return err
	}

	if isIrreversible {
		if strings.TrimSpace(irreversibleReason) == "" {
			return fmt.Errorf("%w: alasan irreversible wajib diisi", apperr.ErrInvalidInput)
		}
		return nil
	}

	return validateSQL(sqlDown, "SQL down")
}

func validateSQL(sql, field string) error {
	switch {
	case strings.TrimSpace(sql) == "":
		return fmt.Errorf("%w: %s tidak boleh kosong", apperr.ErrInvalidInput, field)
	case len(sql) > maxSQLBytes:
		return fmt.Errorf("%w: %s terlalu besar (maks 1 MiB)", apperr.ErrInvalidInput, field)
	case txControl.MatchString(maskSQLNonCode(sql)):
		return fmt.Errorf(
			"%w: jangan sertakan perintah kontrol transaksi; platform mengelola transaksi",
			apperr.ErrInvalidInput,
		)
	default:
		return nil
	}
}

func maskSQLNonCode(sql string) string {
	masked := []byte(sql)

	for i := 0; i < len(sql); {
		switch {
		case strings.HasPrefix(sql[i:], "--"):
			end := i + 2
			for end < len(sql) && sql[end] != '\n' {
				end++
			}
			maskRange(masked, i, end, false)
			i = end

		case strings.HasPrefix(sql[i:], "/*"):
			end := scanBlockComment(sql, i)
			maskRange(masked, i, end, false)
			i = end

		case sql[i] == '\'':
			end := scanQuotedString(sql, i, isEscapeString(sql, i))
			maskRange(masked, i, end, true)
			i = end

		case sql[i] == '"':
			end := scanQuotedString(sql, i, false)
			maskRange(masked, i, end, true)
			i = end

		case sql[i] == '$':
			delimiter, ok := dollarQuoteDelimiter(sql, i)
			if !ok {
				i++
				continue
			}

			start := i + len(delimiter)
			offset := strings.Index(sql[start:], delimiter)
			end := len(sql)
			if offset >= 0 {
				end = start + offset + len(delimiter)
			}
			maskRange(masked, i, end, true)
			i = end

		default:
			i++
		}
	}

	return string(masked)
}

func maskRange(sql []byte, start, end int, markToken bool) {
	for i := start; i < end; i++ {
		if sql[i] != '\n' && sql[i] != '\r' {
			sql[i] = ' '
		}
	}
	if markToken && start < end {
		sql[start] = '_'
	}
}

func scanBlockComment(sql string, start int) int {
	depth := 1
	for i := start + 2; i < len(sql); {
		switch {
		case strings.HasPrefix(sql[i:], "/*"):
			depth++
			i += 2
		case strings.HasPrefix(sql[i:], "*/"):
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return len(sql)
}

func scanQuotedString(sql string, start int, escapeBackslash bool) int {
	quote := sql[start]
	for i := start + 1; i < len(sql); {
		if escapeBackslash && sql[i] == '\\' {
			i += 2
			continue
		}
		if sql[i] == quote {
			if i+1 < len(sql) && sql[i+1] == quote {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return len(sql)
}

func isEscapeString(sql string, quote int) bool {
	if quote > 0 && (sql[quote-1] == 'e' || sql[quote-1] == 'E') {
		return quote == 1 || !isSQLIdentifierByte(sql[quote-2])
	}
	if quote > 2 && sql[quote-1] == '&' &&
		(sql[quote-2] == 'u' || sql[quote-2] == 'U') {
		return quote == 3 || !isSQLIdentifierByte(sql[quote-3])
	}
	return false
}

func dollarQuoteDelimiter(sql string, start int) (string, bool) {
	if start+1 < len(sql) && sql[start+1] == '$' {
		return "$$", true
	}

	if start+1 >= len(sql) || !isSQLIdentifierStart(sql[start+1]) {
		return "", false
	}

	end := start + 2
	for end < len(sql) && isSQLIdentifierByte(sql[end]) {
		end++
	}
	if end < len(sql) && sql[end] == '$' {
		return sql[start : end+1], true
	}
	return "", false
}

func isSQLIdentifierStart(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func isSQLIdentifierByte(b byte) bool {
	return isSQLIdentifierStart(b) || b >= '0' && b <= '9' || b == '$'
}
