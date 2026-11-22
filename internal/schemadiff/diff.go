package schemadiff

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
)

// Satu langkah perubahan DDL hasil diff skema.
type Change struct {
	Object      string `json:"object"`
	Description string `json:"description"`
	SQL         string `json:"sql"`
	Destructive bool   `json:"destructive"`
	Manual      bool   `json:"manual"`
}

// Daftar perubahan dan SQL siap jalan hasil perbandingan skema.
type Result struct {
	Changes []Change `json:"changes"`
	SQL     string   `json:"sql"`
	Manual  bool     `json:"manual"`
}

type diffBuilder struct {
	driver        string
	currentSchema string
	desiredSchema string
	drops         []Change
	tableDrops    []Change
	creates       []Change
	alters        []Change
	constraints   []Change
	indexes       []Change
	manualTables  map[string]bool
}

// Bandingkan dua snapshot skema lalu susun langkah migrasi DDL-nya.
func Compare(current, desired targetdb.Snapshot, currentSchema, desiredSchema string, allowDestructive bool) (Result, error) {
	result := Result{Changes: []Change{}}
	if current.Driver != desired.Driver {
		return result, fmt.Errorf("perbandingan lintas DBMS tidak didukung")
	}
	if !slices.Contains(current.Schemas(), currentSchema) {
		return result, fmt.Errorf("schema target %q tidak ditemukan", currentSchema)
	}
	if !slices.Contains(desired.Schemas(), desiredSchema) {
		return result, fmt.Errorf("schema pembanding %q tidak ditemukan", desiredSchema)
	}

	before := indexTablesByName(current.Tables, currentSchema)
	after := indexTablesByName(desired.Tables, desiredSchema)
	names := collectSortedTableNames(before, after)

	builder := &diffBuilder{
		driver:        current.Driver,
		currentSchema: currentSchema,
		desiredSchema: desiredSchema,
		manualTables:  map[string]bool{},
	}

	for _, name := range names {
		old, exists := before[name]
		next, wanted := after[name]
		if !wanted {
			builder.addDroppedTable(name, old)
			continue
		}
		if !exists {
			builder.addCreatedTable(name, next)
		} else {
			builder.diffExistingTable(name, old, next)
		}
		builder.diffIndexes(name, old, next)
	}

	return builder.buildResult(allowDestructive), nil
}

// Indeks daftar tabel dalam satu schema menurut namanya.
func indexTablesByName(tables []targetdb.Table, schema string) map[string]targetdb.Table {
	indexed := map[string]targetdb.Table{}
	for _, table := range tables {
		if table.Schema == schema {
			indexed[table.Name] = table
		}
	}
	return indexed
}

// Gabungkan dan urutkan seluruh nama tabel kedua snapshot.
func collectSortedTableNames(before, after map[string]targetdb.Table) []string {
	names := make([]string, 0, len(before)+len(after))
	for name := range before {
		names = append(names, name)
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Kualifikasi nama objek dengan nama schema target.
func (b *diffBuilder) qualify(name string) string {
	return targetdb.Quote(b.currentSchema) + "." + targetdb.Quote(name)
}

// Arahkan qualifier schema acuan ke schema target.
func (b *diffBuilder) remap(sql string) string {
	return rewriteSchema(sql, b.desiredSchema, b.currentSchema)
}

// Bandingkan definisi SQL setelah qualifier schema keduanya disamakan.
func (b *diffBuilder) same(a, c string) bool {
	return rewriteSchema(a, b.currentSchema, b.currentSchema) == b.remap(c)
}

// Catat penghapusan FK dan DROP TABLE untuk tabel yang hilang.
func (b *diffBuilder) addDroppedTable(name string, old targetdb.Table) {
	table := b.qualify(name)
	if b.driver == "postgres" {
		for _, constraint := range old.Constraints {
			if constraint.Kind == "f" {
				b.drops = append(b.drops, Change{
					Object:      name + "." + constraint.Name,
					Description: "Hapus foreign key",
					SQL:         "ALTER TABLE " + table + " DROP CONSTRAINT " + targetdb.Quote(constraint.Name),
					Destructive: true,
				})
			}
		}
	}
	b.tableDrops = append(b.tableDrops, Change{
		Object:      name,
		Description: "Hapus tabel",
		SQL:         "DROP TABLE " + table,
		Destructive: true,
	})
}

// Catat CREATE TABLE dan constraint awal untuk tabel baru.
func (b *diffBuilder) addCreatedTable(name string, next targetdb.Table) {
	if b.driver == "sqlite" {
		b.creates = append(b.creates, Change{
			Object:      name,
			Description: "Buat tabel",
			SQL:         next.Definition,
			Manual:      next.Definition == "",
		})
		return
	}

	table := b.qualify(name)
	cols := make([]string, 0, len(next.Columns))
	manual := false
	for _, col := range next.Columns {
		cols = append(cols, columnSQL(col))
		if col.Generated != "" ||
			(col.Default != nil && strings.Contains(strings.ToLower(*col.Default), "nextval(")) ||
			strings.Contains(col.Type, ".") {
			manual = true
		}
	}

	b.creates = append(b.creates, Change{
		Object:      name,
		Description: "Buat tabel",
		SQL:         "CREATE TABLE " + table + " (" + strings.Join(cols, ", ") + ")",
		Manual:      manual,
	})
	b.manualTables[name] = manual

	for _, constraint := range next.Constraints {
		b.constraints = append(b.constraints, Change{
			Object:      name + "." + constraint.Name,
			Description: "Tambah constraint",
			SQL:         "ALTER TABLE " + table + " ADD CONSTRAINT " + targetdb.Quote(constraint.Name) + " " + b.remap(constraint.Definition),
		})
	}
}

// Bandingkan kolom dan constraint tabel yang ada di kedua snapshot.
func (b *diffBuilder) diffExistingTable(name string, old, next targetdb.Table) {
	table := b.qualify(name)
	b.diffColumns(name, table, old, next)
	b.diffConstraints(name, table, old, next)
}

// Deteksi kolom baru, berubah, atau terhapus pada satu tabel.
func (b *diffBuilder) diffColumns(name, table string, old, next targetdb.Table) {
	oldCols := map[string]targetdb.Column{}
	newCols := map[string]targetdb.Column{}
	for _, col := range old.Columns {
		oldCols[col.Name] = col
	}
	for _, col := range next.Columns {
		newCols[col.Name] = col
	}

	for _, col := range next.Columns {
		previous, ok := oldCols[col.Name]
		if !ok {
			b.alters = append(b.alters, Change{
				Object:      name + "." + col.Name,
				Description: "Tambah kolom",
				SQL:         "ALTER TABLE " + table + " ADD COLUMN " + columnSQL(col),
				Manual:      col.Generated != "" || col.Identity != "",
			})
			continue
		}
		if reflect.DeepEqual(previous, col) {
			continue
		}
		if b.driver == "sqlite" {
			b.alters = append(b.alters, Change{
				Object:      name + "." + col.Name,
				Description: "Ubah kolom (SQL manual/rebuild tabel)",
				Manual:      true,
			})
			continue
		}
		b.diffPostgresColumn(name, table, previous, col)
	}

	for _, col := range old.Columns {
		if _, ok := newCols[col.Name]; !ok {
			b.alters = append(b.alters, Change{
				Object:      name + "." + col.Name,
				Description: "Hapus kolom",
				SQL:         "ALTER TABLE " + table + " DROP COLUMN " + targetdb.Quote(col.Name),
				Destructive: true,
				Manual:      b.driver == "sqlite",
			})
		}
	}
}

// Susun ALTER COLUMN untuk perubahan kolom PostgreSQL.
func (b *diffBuilder) diffPostgresColumn(name, table string, previous, nextCol targetdb.Column) {
	object := name + "." + nextCol.Name
	prefix := "ALTER TABLE " + table + " ALTER COLUMN " + targetdb.Quote(nextCol.Name)

	if previous.Type != nextCol.Type {
		b.alters = append(b.alters, Change{
			Object:      object,
			Description: "Ubah tipe (periksa konversi data)",
			SQL:         prefix + " TYPE " + nextCol.Type,
			Destructive: true,
		})
	}
	if previous.Nullable != nextCol.Nullable {
		suffix := " SET NOT NULL"
		if nextCol.Nullable {
			suffix = " DROP NOT NULL"
		}
		b.alters = append(b.alters, Change{
			Object:      object,
			Description: "Ubah nullable",
			SQL:         prefix + suffix,
		})
	}
	if !reflect.DeepEqual(previous.Default, nextCol.Default) {
		suffix := " DROP DEFAULT"
		if nextCol.Default != nil {
			suffix = " SET DEFAULT " + *nextCol.Default
		}
		b.alters = append(b.alters, Change{
			Object:      object,
			Description: "Ubah default",
			SQL:         prefix + suffix,
			Manual:      nextCol.Identity != "" || nextCol.Generated != "",
		})
	}
	if previous.Identity != nextCol.Identity || previous.Generated != nextCol.Generated {
		b.alters = append(b.alters, Change{
			Object:      object,
			Description: "Perubahan identity/generated memerlukan SQL manual",
			Manual:      true,
		})
	}
}

// Bandingkan constraint tabel yang berubah, dihapus, atau ditambah.
func (b *diffBuilder) diffConstraints(name, table string, old, next targetdb.Table) {
	if b.driver == "sqlite" {
		if !reflect.DeepEqual(old.Constraints, next.Constraints) {
			b.constraints = append(b.constraints, Change{
				Object:      name,
				Description: "Perubahan constraint memerlukan SQL manual/rebuild tabel",
				Manual:      true,
			})
		}
		return
	}

	oldCons := map[string]targetdb.Constraint{}
	newCons := map[string]targetdb.Constraint{}
	for _, constraint := range old.Constraints {
		oldCons[constraint.Name] = constraint
	}
	for _, constraint := range next.Constraints {
		newCons[constraint.Name] = constraint
	}

	for _, constraint := range old.Constraints {
		nextCon, ok := newCons[constraint.Name]
		if !ok || !b.same(constraint.Definition, nextCon.Definition) {
			b.drops = append(b.drops, Change{
				Object:      name + "." + constraint.Name,
				Description: "Hapus constraint",
				SQL:         "ALTER TABLE " + table + " DROP CONSTRAINT " + targetdb.Quote(constraint.Name),
				Destructive: true,
			})
		}
	}
	for _, constraint := range next.Constraints {
		oldCon, ok := oldCons[constraint.Name]
		if !ok || !b.same(oldCon.Definition, constraint.Definition) {
			b.constraints = append(b.constraints, Change{
				Object:      name + "." + constraint.Name,
				Description: "Tambah constraint",
				SQL:         "ALTER TABLE " + table + " ADD CONSTRAINT " + targetdb.Quote(constraint.Name) + " " + b.remap(constraint.Definition),
				Destructive: ok,
			})
		}
	}
}

// Bandingkan index non-constraint untuk dihapus atau dibuat ulang.
func (b *diffBuilder) diffIndexes(name string, old, next targetdb.Table) {
	oldIndexes := map[string]targetdb.Index{}
	newIndexes := map[string]targetdb.Index{}
	for _, index := range old.Indexes {
		if !index.Constraint {
			oldIndexes[index.Name] = index
		}
	}
	for _, index := range next.Indexes {
		if !index.Constraint {
			newIndexes[index.Name] = index
		}
	}

	for _, index := range old.Indexes {
		if index.Constraint {
			continue
		}
		nextIdx, ok := newIndexes[index.Name]
		if !ok || !b.same(index.Definition, nextIdx.Definition) {
			b.drops = append(b.drops, Change{
				Object:      name + "." + index.Name,
				Description: "Hapus index",
				SQL:         "DROP INDEX " + b.qualify(index.Name),
				Destructive: true,
			})
		}
	}
	for _, index := range next.Indexes {
		if index.Constraint {
			continue
		}
		oldIdx, ok := oldIndexes[index.Name]
		if !ok || !b.same(oldIdx.Definition, index.Definition) {
			b.indexes = append(b.indexes, Change{
				Object:      name + "." + index.Name,
				Description: "Tambah index",
				SQL:         b.remap(index.Definition),
				Manual:      index.Definition == "",
				Destructive: ok,
			})
		}
	}
}

// Gabungkan seluruh perubahan sesuai urutan eksekusi dan susun SQL finalnya.
func (b *diffBuilder) buildResult(allowDestructive bool) Result {
	result := Result{Changes: []Change{}}
	result.Changes = append(result.Changes, b.drops...)
	result.Changes = append(result.Changes, b.tableDrops...)
	result.Changes = append(result.Changes, b.creates...)
	result.Changes = append(result.Changes, b.alters...)
	result.Changes = append(result.Changes, b.constraints...)
	result.Changes = append(result.Changes, b.indexes...)

	statements := []string{}
	for index, change := range result.Changes {
		for table, manual := range b.manualTables {
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
		statements = append(statements, change.SQL+";")
	}

	result.SQL = strings.Join(statements, "\n")
	return result
}

// Susun definisi SQL untuk satu kolom tabel.
func columnSQL(col targetdb.Column) string {
	sql := targetdb.Quote(col.Name) + " " + col.Type
	if col.Identity != "" {
		kind := "BY DEFAULT"
		if col.Identity == "a" {
			kind = "ALWAYS"
		}
		sql += " GENERATED " + kind + " AS IDENTITY"
	} else if col.Default != nil {
		sql += " DEFAULT " + *col.Default
	}
	if !col.Nullable {
		sql += " NOT NULL"
	}
	return sql
}
