package schemadiff

import (
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
	"strings"
)

// Rewrite qualifiers without changing string literals in CHECK expressions or indexes.
func rewriteSchema(sql, from, to string) string {
	var result strings.Builder
	for i := 0; i < len(sql); {
		start := i
		if sql[i] == '\'' {
			i++
			for i < len(sql) {
				if sql[i] == '\'' {
					i++
					if i < len(sql) && sql[i] == '\'' {
						i++
						continue
					}
					break
				}
				i++
			}
			result.WriteString(sql[start:i])
			continue
		}
		name := ""
		if sql[i] == '"' {
			i++
			var value strings.Builder
			for i < len(sql) {
				if sql[i] == '"' {
					i++
					if i < len(sql) && sql[i] == '"' {
						value.WriteByte('"')
						i++
						continue
					}
					break
				}
				value.WriteByte(sql[i])
				i++
			}
			name = value.String()
		} else if word(sql[i]) {
			for i < len(sql) && word(sql[i]) {
				i++
			}
			name = sql[start:i]
		} else {
			result.WriteByte(sql[i])
			i++
			continue
		}
		next := i
		for next < len(sql) && (sql[next] == ' ' || sql[next] == '\t') {
			next++
		}
		if name == from && next < len(sql) && sql[next] == '.' {
			result.WriteString(targetdb.Quote(to))
		} else {
			result.WriteString(sql[start:i])
		}
	}
	return result.String()
}

func word(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '$'
}
