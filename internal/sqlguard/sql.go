package sqlguard

import (
	"errors"
	"strings"
	"unicode"
)

const (
	maxSQLBytes       = 65536
	maxStatementCount = 100
)

// Satu statement SQL beserta token huruf besarnya.
type Statement struct {
	SQL    string
	Tokens []string
}

// Pecah SQL di tanda ';' tanpa memotong komentar, kutip, dan dollar-quote.
func Split(sql string) ([]Statement, error) {
	if len(sql) > maxSQLBytes {
		return nil, errors.New("SQL dibatasi 64 KiB")
	}

	statements := []Statement{}
	tokens := []string{}
	start := 0

	appendStatement := func(end int) {
		if len(tokens) == 0 {
			return
		}
		statements = append(statements, Statement{
			SQL:    strings.TrimSpace(sql[start:end]),
			Tokens: tokens,
		})
		tokens = []string{}
	}

	for i := 0; i < len(sql); {
		ch := sql[i]
		if unicode.IsSpace(rune(ch)) {
			i++
			continue
		}

		if ch == ';' {
			appendStatement(i)
			i++
			start = i
			continue
		}

		// Lewati komentar satu baris tanpa menambah token.
		if i+1 < len(sql) && sql[i:i+2] == "--" {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		}

		// Tolak komentar blok bersarang karena perilaku PostgreSQL dan SQLite berbeda.
		if i+1 < len(sql) && sql[i:i+2] == "/*" {
			i += 2
			closed := false
			for i+1 < len(sql) {
				if sql[i:i+2] == "/*" {
					return nil, errors.New("komentar SQL bersarang belum didukung")
				}
				if sql[i:i+2] == "*/" {
					i += 2
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, errors.New("komentar SQL tidak ditutup")
			}
			continue
		}

		if ch == '\'' || ch == '"' {
			quote := ch
			i++
			var value strings.Builder
			closed := false
			for i < len(sql) {
				// Tolak escape backslash di literal agar perilaku kutip antar dialect konsisten.
				if sql[i] == '\\' && quote == '\'' {
					return nil, errors.New("escape backslash dalam literal belum didukung; gunakan kutip ganda SQL untuk apostrof")
				}
				if sql[i] == quote {
					if i+1 < len(sql) && sql[i+1] == quote {
						value.WriteByte(quote)
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				value.WriteByte(sql[i])
				i++
			}
			if !closed {
				return nil, errors.New("literal atau identifier tidak ditutup")
			}
			if quote == '"' {
				tokens = append(tokens, strings.ToUpper(value.String()))
			} else {
				tokens = append(tokens, "<literal>")
			}
			continue
		}

		// Tangani string dollar-quote PostgreSQL ($tag$...$tag$) sebagai satu literal utuh.
		if ch == '$' {
			tagEnd := i + 1
			for tagEnd < len(sql) && (isIdentifierChar(sql[tagEnd]) || sql[tagEnd] == '_') {
				tagEnd++
			}
			if tagEnd < len(sql) && sql[tagEnd] == '$' {
				tag := sql[i : tagEnd+1]
				closeOffset := strings.Index(sql[tagEnd+1:], tag)
				if closeOffset < 0 {
					return nil, errors.New("dollar quote tidak ditutup")
				}
				i = tagEnd + 1 + closeOffset + len(tag)
				tokens = append(tokens, "<literal>")
				continue
			}
		}

		if isIdentifierChar(ch) {
			end := i + 1
			for end < len(sql) && (isIdentifierChar(sql[end]) || sql[end] == '$') {
				end++
			}
			tokens = append(tokens, strings.ToUpper(sql[i:end]))
			i = end
			continue
		}

		if ch >= 128 {
			return nil, errors.New("gunakan quoted identifier untuk nama non-ASCII")
		}

		tokens = append(tokens, string(ch))
		i++
	}

	appendStatement(len(sql))

	if len(statements) == 0 {
		return nil, errors.New("SQL tidak boleh kosong")
	}
	if len(statements) > maxStatementCount {
		return nil, errors.New("maksimum 100 statement per migration")
	}

	return statements, nil
}

// Cek apakah karakter valid untuk identifier SQL ASCII.
func isIdentifierChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_'
}

// Pastikan seluruh statement masuk allowlist migration transaksional.
func Migration(sql string) ([]Statement, error) {
	statements, err := Split(sql)
	if err != nil {
		return nil, err
	}

	for _, stmt := range statements {
		err = checkTokens(stmt.Tokens)
		if err != nil {
			return nil, err
		}

		tokens := stmt.Tokens
		allowed := false
		switch tokens[0] {
		case "SELECT", "INSERT", "UPDATE", "DELETE", "WITH":
			allowed = true
		case "CREATE":
			allowed = len(tokens) > 1 && (tokens[1] == "TABLE" ||
				tokens[1] == "INDEX" ||
				(len(tokens) > 2 && tokens[1] == "UNIQUE" && tokens[2] == "INDEX"))
		case "ALTER":
			allowed = len(tokens) > 1 && tokens[1] == "TABLE"
		case "DROP":
			allowed = len(tokens) > 1 && (tokens[1] == "TABLE" || tokens[1] == "INDEX")
		}

		if !allowed {
			return nil, errors.New("statement tidak didukung oleh migration transaksional")
		}
	}

	return statements, nil
}

// Validasi input sebagai tepat satu query/DML untuk preview EXPLAIN.
func Plan(sql string) (Statement, error) {
	statements, err := Migration(sql)
	if err != nil {
		return Statement{}, err
	}
	if len(statements) != 1 {
		return Statement{}, errors.New("preview plan membutuhkan tepat satu statement")
	}

	stmt := statements[0]
	switch stmt.Tokens[0] {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "WITH":
		return stmt, nil
	}

	return Statement{}, errors.New("execution plan hanya tersedia untuk SELECT, INSERT, UPDATE, DELETE, dan WITH")
}

// Blokir kontrol transaksi manual, akses ledger, dan fungsi admin berbahaya.
func checkTokens(tokens []string) error {
	for _, token := range tokens {
		switch token {
		case "SCHEMA_MIGRATIONS",
			"BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE", "RESET",
			"PRAGMA", "ATTACH", "DETACH", "VACUUM",
			"COPY", "CALL", "DO", "SET_CONFIG",
			"LOAD_EXTENSION", "READFILE", "WRITEFILE",
			"CONCURRENTLY", "PROGRAM", "DBLINK":
			return errors.New("SQL berisi operasi terlarang atau objek internal platform")
		}

		if strings.HasPrefix(token, "PG_") || strings.HasPrefix(token, "LO_") || strings.HasPrefix(token, "DBLINK") {
			return errors.New("fungsi administratif database tidak diperbolehkan")
		}
	}

	return nil
}
