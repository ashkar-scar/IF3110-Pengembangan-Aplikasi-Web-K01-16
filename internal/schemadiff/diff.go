package schemadiff

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
)

type Change struct {
	Object      string
	Description string
	SQL         string
	Destructive bool
	Manual      bool
}

type Result struct {
	Changes []Change
	SQL     string
	Manual  bool
}

func Compare(current, desired targetdb.Snapshot, currentSchema, desiredSchema string, allowDestructive bool) (Result, error) {
	result := Result{Changes: []Change{}}
	if current.Driver != desired.Driver {
		return result, fmt.Errorf("perbandingan lintas DBMS tidak didukung")
	}

	before, after := map[string]targetdb.Table{}, map[string]targetdb.Table{}
	for _, t := range current.Tables {
		if t.Schema == currentSchema {
			before[t.Name] = t
		}
	}

	for _, t := range desired.Tables {
		if t.Schema == desiredSchema {
			after[t.Name] = t
		}
	}

	names := []string{}
	for name := range before {
		names = append(names, name)
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			names = append(names, name)
		}
	}

	sort.Strings(names)
	var drops, tableDrops, creates, alters, constraints, indexes []Change
	manualTables := map[string]bool{}
	qualify := func(name string) string { return targetdb.Quote(currentSchema) + "." + targetdb.Quote(name) }
	remap := func(sql string) string {
		return rewriteSchema(sql, desiredSchema, currentSchema)
	}
	same := func(a, b string) bool { return rewriteSchema(a, currentSchema, currentSchema) == remap(b) }
	for _, name := range names {
		old, exists := before[name]
		next, wanted := after[name]
		table := qualify(name)
		if !wanted {
			if current.Driver == "postgres" {
				for _, c := range old.Constraints {
					if c.Kind == "f" {
						drops = append(drops, Change{Object: name + "." + c.Name, Description: "Hapus foreign key", SQL: "ALTER TABLE " + table + " DROP CONSTRAINT " + targetdb.Quote(c.Name), Destructive: true})
					}
				}
			}
			tableDrops = append(tableDrops, Change{Object: name, Description: "Hapus tabel", SQL: "DROP TABLE " + table, Destructive: true})
			continue
		}
		if !exists {
			if current.Driver == "sqlite" {
				creates = append(creates, Change{Object: name, Description: "Buat tabel", SQL: next.Definition, Manual: next.Definition == ""})
			} else {
				cols := []string{}
				manual := false
				for _, col := range next.Columns {
					cols = append(cols, columnSQL(col))
					if col.Generated != "" || (col.Default != nil && strings.Contains(strings.ToLower(*col.Default), "nextval(")) || strings.Contains(col.Type, ".") {
						manual = true
					}
				}

				creates = append(creates, Change{Object: name, Description: "Buat tabel", SQL: "CREATE TABLE " + table + " (" + strings.Join(cols, ", ") + ")", Manual: manual})
				manualTables[name] = manual
				for _, con := range next.Constraints {
					constraints = append(constraints, Change{Object: name + "." + con.Name, Description: "Tambah constraint", SQL: "ALTER TABLE " + table + " ADD CONSTRAINT " + targetdb.Quote(con.Name) + " " + remap(con.Definition)})
				}
			}
		} else {
			oldCols, newCols := map[string]targetdb.Column{}, map[string]targetdb.Column{}
			for _, c := range old.Columns {
				oldCols[c.Name] = c
			}
			for _, c := range next.Columns {
				newCols[c.Name] = c
			}
			for _, c := range next.Columns {
				previous, ok := oldCols[c.Name]
				if !ok {
					alters = append(alters, Change{Object: name + "." + c.Name, Description: "Tambah kolom", SQL: "ALTER TABLE " + table + " ADD COLUMN " + columnSQL(c), Manual: c.Generated != "" || c.Identity != ""})
					continue
				}
				if reflect.DeepEqual(previous, c) {
					continue
				}
				if current.Driver == "sqlite" {
					alters = append(alters, Change{Object: name + "." + c.Name, Description: "Ubah kolom (SQL manual/rebuild tabel)", Manual: true})
					continue
				}

				prefix := "ALTER TABLE " + table + " ALTER COLUMN " + targetdb.Quote(c.Name)
				if previous.Type != c.Type {
					alters = append(alters, Change{Object: name + "." + c.Name, Description: "Ubah tipe (periksa konversi data)", SQL: prefix + " TYPE " + c.Type, Destructive: true})
				}
				if previous.Nullable != c.Nullable {
					suffix := " SET NOT NULL"
					if c.Nullable {
						suffix = " DROP NOT NULL"
					}
					alters = append(alters, Change{Object: name + "." + c.Name, Description: "Ubah nullable", SQL: prefix + suffix})
				}
				if !reflect.DeepEqual(previous.Default, c.Default) {
					suffix := " DROP DEFAULT"
					if c.Default != nil {
						suffix = " SET DEFAULT " + *c.Default
					}
					alters = append(alters, Change{Object: name + "." + c.Name, Description: "Ubah default", SQL: prefix + suffix, Manual: c.Identity != "" || c.Generated != ""})
				}
				if previous.Identity != c.Identity || previous.Generated != c.Generated {
					alters = append(alters, Change{Object: name + "." + c.Name, Description: "Perubahan identity/generated memerlukan SQL manual", Manual: true})
				}
			}
			for _, c := range old.Columns {
				if _, ok := newCols[c.Name]; !ok {
					alters = append(alters, Change{Object: name + "." + c.Name, Description: "Hapus kolom", SQL: "ALTER TABLE " + table + " DROP COLUMN " + targetdb.Quote(c.Name), Destructive: true, Manual: current.Driver == "sqlite"})
				}
			}
			if current.Driver == "sqlite" {
				if !reflect.DeepEqual(old.Constraints, next.Constraints) {
					constraints = append(constraints, Change{Object: name, Description: "Perubahan constraint memerlukan SQL manual/rebuild tabel", Manual: true})
				}
			} else {
				oldCons, newCons := map[string]targetdb.Constraint{}, map[string]targetdb.Constraint{}
				for _, c := range old.Constraints {
					oldCons[c.Name] = c
				}
				for _, c := range next.Constraints {
					newCons[c.Name] = c
				}
				for _, c := range old.Constraints {
					n, ok := newCons[c.Name]
					if !ok || !same(c.Definition, n.Definition) {
						drops = append(drops, Change{Object: name + "." + c.Name, Description: "Hapus constraint", SQL: "ALTER TABLE " + table + " DROP CONSTRAINT " + targetdb.Quote(c.Name), Destructive: true})
					}
				}
				for _, c := range next.Constraints {
					o, ok := oldCons[c.Name]
					if !ok || !same(o.Definition, c.Definition) {
						constraints = append(constraints, Change{Object: name + "." + c.Name, Description: "Tambah constraint", SQL: "ALTER TABLE " + table + " ADD CONSTRAINT " + targetdb.Quote(c.Name) + " " + remap(c.Definition), Destructive: ok})
					}
				}
			}
		}
		oldIndexes, newIndexes := map[string]targetdb.Index{}, map[string]targetdb.Index{}
		for _, i := range old.Indexes {
			if !i.Constraint {
				oldIndexes[i.Name] = i
			}
		}
		for _, i := range next.Indexes {
			if !i.Constraint {
				newIndexes[i.Name] = i
			}
		}
		for _, i := range old.Indexes {
			if i.Constraint {
				continue
			}
			n, ok := newIndexes[i.Name]
			if !ok || !same(i.Definition, n.Definition) {
				drops = append(drops, Change{Object: name + "." + i.Name, Description: "Hapus index", SQL: "DROP INDEX " + qualify(i.Name), Destructive: true})
			}
		}
		for _, i := range next.Indexes {
			if i.Constraint {
				continue
			}

			o, ok := oldIndexes[i.Name]
			if !ok || !same(o.Definition, i.Definition) {
				indexes = append(indexes, Change{Object: name + "." + i.Name, Description: "Tambah index", SQL: remap(i.Definition), Manual: i.Definition == "", Destructive: ok})
			}
		}
	}
	result.Changes = append(result.Changes, drops...)
	result.Changes = append(result.Changes, tableDrops...)
	result.Changes = append(result.Changes, creates...)
	result.Changes = append(result.Changes, alters...)
	result.Changes = append(result.Changes, constraints...)
	result.Changes = append(result.Changes, indexes...)
	sql := []string{}
	for index, change := range result.Changes {
		for table, manual := range manualTables {
			if manual && (change.Object == table || strings.HasPrefix(change.Object, table+".")) {
				change.Manual = true
			}
		}
		result.Changes[index] = change
		if change.Manual {
			result.Manual = true
			continue
		}
		if change.Destructive && !allowDestructive {
			continue
		}
		sql = append(sql, change.SQL+";")
	}
	result.SQL = strings.Join(sql, "\n")
	return result, nil
}

func columnSQL(c targetdb.Column) string {
	sql := targetdb.Quote(c.Name) + " " + c.Type
	if c.Identity != "" {
		kind := "BY DEFAULT"
		if c.Identity == "a" {
			kind = "ALWAYS"
		}
		sql += " GENERATED " + kind + " AS IDENTITY"
	} else if c.Default != nil {
		sql += " DEFAULT " + *c.Default
	}
	if !c.Nullable {
		sql += " NOT NULL"
	}
	return sql
}
