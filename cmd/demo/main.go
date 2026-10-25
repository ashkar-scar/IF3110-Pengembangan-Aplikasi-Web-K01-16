package main

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := store.NewPostgresPool(ctx, os.Getenv("DATABASE_URL"))
	check(err)
	defer pool.Close()
	check(store.SetupPlatform(ctx, pool))
	password := os.Getenv("DEMO_PASSWORD")
	if len(password) < 10 || len(password) > 72 {
		log.Fatal("DEMO_PASSWORD harus 10-72 byte")
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("TARGET_CREDENTIAL_KEY"))
	check(err)
	registry, err := store.NewTargetRepository(pool, key)
	check(err)
	users := store.NewUserRepository(pool)
	groups := store.NewGroupRepository(pool)
	grants := store.NewPermissionRepository(pool)
	seedUsers := map[string]core.User{}
	for _, name := range []string{"demo-editor", "demo-viewer"} {
		u, err := users.FindByUsername(ctx, name)
		if errors.Is(err, pgx.ErrNoRows) {
			hash, e := auth.CreatePasswordHash(password)
			check(e)
			u, err = users.Create(ctx, core.User{Username: name, PasswordHash: hash, Active: true})
		}
		check(err)
		seedUsers[name] = u
	}
	adminName := os.Getenv("ADMIN_USERNAME")
	if adminName == "" {
		adminName = "admin"
	}
	admin, err := users.FindByUsername(ctx, adminName)
	check(err)
	allGroups, err := groups.FindAll(ctx)
	check(err)
	groupIDs := map[string]int64{}
	for _, g := range allGroups {
		groupIDs[g.Name] = g.ID
	}
	for _, name := range []string{"Demo Editors", "Demo Viewers"} {
		if groupIDs[name] == 0 {
			g, e := groups.Create(ctx, core.Group{Name: name})
			check(e)
			groupIDs[name] = g.ID
		}
	}
	for _, member := range [][2]int64{{groupIDs["Demo Editors"], seedUsers["demo-editor"].ID}, {groupIDs["Demo Editors"], admin.ID}, {groupIDs["Demo Viewers"], seedUsers["demo-viewer"].ID}} {
		err := groups.AddMember(ctx, member[0], member[1])
		var pgerr *pgconn.PgError
		if err != nil && !(errors.As(err, &pgerr) && pgerr.Code == "23505") {
			check(err)
		}
	}
	host := os.Getenv("DEMO_TARGET_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := 54329
	if value := os.Getenv("DEMO_TARGET_PORT"); value != "" {
		port, err = strconv.Atoi(value)
		check(err)
	}
	targets, err := registry.FindAll(ctx)
	check(err)
	existing := map[string]core.DatabaseTarget{}
	for _, t := range targets {
		existing[t.Name] = t
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = ".local/data"
	}
	for _, record := range []core.DatabaseTarget{
		{Name: "Demo Sales", Driver: "postgres", Host: host, Port: port, Database: "sales", Username: "target_user", Password: "target_dev", SSLMode: "disable", Active: true},
		{Name: "Demo HR", Driver: "postgres", Host: host, Port: port, Database: "hr", Username: "target_user", Password: "target_dev", SSLMode: "disable", Active: true},
		{Name: "Demo Notes", Driver: "sqlite", Active: true},
	} {
		if record.Name == "Demo Sales" && os.Getenv("DEMO_SALES_DATABASE") != "" {
			record.Database = os.Getenv("DEMO_SALES_DATABASE")
		}
		if record.Name == "Demo HR" && os.Getenv("DEMO_HR_DATABASE") != "" {
			record.Database = os.Getenv("DEMO_HR_DATABASE")
		}
		target, ok := existing[record.Name]
		if !ok {
			target, err = registry.Create(ctx, record)
			check(err)
		}
		if !target.Active {
			continue
		}
		if target.Driver == "sqlite" {
			conn, e := targetdb.OpenSQLite(ctx, dataDir, target.Database)
			check(e)
			version, e := conn.Version(ctx)
			check(e)
			if version == 0 {
				_, e = conn.Execute(ctx, targetdb.Execution{ID: "demo-notes-seed", ActorID: admin.ID, ActorName: admin.Username, SQL: "CREATE TABLE notes(id INTEGER PRIMARY KEY, title TEXT NOT NULL, body TEXT); INSERT INTO notes VALUES (1,'Catatan demo','SQLite siap digunakan'),(2,'Migration','Uji commit dan rollback')"})
				check(e)
			}
			conn.Close()
		}
		check(grants.SetGrant(ctx, core.DatabaseGrant{GroupID: groupIDs["Demo Editors"], DatabaseID: target.ID, Permission: core.PermissionEdit}))
		if target.Name != "Demo HR" {
			check(grants.SetGrant(ctx, core.DatabaseGrant{GroupID: groupIDs["Demo Viewers"], DatabaseID: target.ID, Permission: core.PermissionView}))
		}
	}
	log.Print("Demo siap: admin, demo-editor, demo-viewer; PostgreSQL sales/hr dan SQLite notes. Password demo berada di .env lokal.")
}
