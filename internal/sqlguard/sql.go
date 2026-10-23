package sqlguard

import (
	"errors"
	"strings"
	"unicode"
)

type Statement struct {
	SQL    string
	Tokens []string
}

func Split(sql string) ([]Statement, error) {
	if len(sql) > 65536 {
		return nil, errors.New("SQL dibatasi 64 KiB")
	}
	statements := []Statement{}
	tokens := []string{}
	start := 0
	appendStatement := func(end int) {
		if len(tokens) > 0 {
			statements = append(statements, Statement{SQL: strings.TrimSpace(sql[start:end]), Tokens: tokens})
			tokens = []string{}
		}
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
		if i+1 < len(sql) && sql[i:i+2] == "--" {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		}
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
		if ch == '$' {
			j := i + 1
			for j < len(sql) && (word(sql[j]) || sql[j] == '_') {
				j++
			}
			if j < len(sql) && sql[j] == '$' {
				tag := sql[i : j+1]
				end := strings.Index(sql[j+1:], tag)
				if end < 0 {
					return nil, errors.New("dollar quote tidak ditutup")
				}
				i = j + 1 + end + len(tag)
				tokens = append(tokens, "<literal>")
				continue
			}
		}
		if word(ch) {
			j := i + 1
			for j < len(sql) && (word(sql[j]) || sql[j] == '$') {
				j++
			}
			tokens = append(tokens, strings.ToUpper(sql[i:j]))
			i = j
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
	if len(statements) > 100 {
		return nil, errors.New("maksimum 100 statement per migration")
	}
	return statements, nil
}

func word(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_'
}
