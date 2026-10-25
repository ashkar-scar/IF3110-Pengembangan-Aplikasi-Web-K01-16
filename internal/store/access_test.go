package store_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func user(t *testing.T, pool *pgxpool.Pool, name string) core.User {
	t.Helper()
	u, err := store.NewUserRepository(pool).Create(context.Background(), core.User{Username: name, PasswordHash: "not-used", Active: true})
	must(t, err)
	return u
}

func newSession(t *testing.T, pool *pgxpool.Pool, userID int64) core.Session {
	t.Helper()
	now := time.Now().UTC()
	s, err := store.NewSessionRepository(pool).Create(context.Background(), core.Session{UserID: userID, CSRFToken: "csrf", CreatedAt: now, LastSeenAt: now, AbsoluteExpiresAt: now.Add(time.Hour), RotateAfter: now.Add(time.Minute)}, []byte(time.Now().String()))
	must(t, err)
	return s
}

func TestGroupAndGrantChangesUpdateSessionsAtomically(t *testing.T) {
	pool := testutil.Platform(t)
	ctx := context.Background()
	groups := store.NewGroupRepository(pool)
	permissions := store.NewPermissionRepository(pool)
	u := user(t, pool, "member")
	other := user(t, pool, "unrelated")
	g, err := groups.Create(ctx, core.Group{Name: "analysts"})
	must(t, err)
	if members, err := groups.FindMembers(ctx, g.ID); err != nil || len(members) != 0 {
		t.Fatal(members, err)
	}
	s := newSession(t, pool, u.ID)
	unrelated := newSession(t, pool, other.ID)
	must(t, groups.AddMember(ctx, g.ID, u.ID))
	var rotate bool
	must(t, pool.QueryRow(ctx, "SELECT rotation_required FROM sessions WHERE id=$1", s.ID).Scan(&rotate))
	if !rotate {
		t.Fatal("membership addition must require rotation")
	}
	if err := groups.AddMember(ctx, g.ID, u.ID); err == nil {
		t.Fatal("duplicate membership accepted")
	}
	members, err := groups.FindMembers(ctx, g.ID)
	must(t, err)
	if len(members) != 1 || members[0].ID != u.ID || members[0].PasswordHash != "" {
		t.Fatal(members)
	}
	registry, err := store.NewTargetRepository(pool, bytes.Repeat([]byte{1}, 32))
	must(t, err)
	target, err := registry.Create(ctx, core.DatabaseTarget{Name: "sales", Driver: "postgres", Host: "127.0.0.1", Port: 54329, Database: "sales", Username: "target_user", SSLMode: "disable", Active: true, Password: "target_dev"})
	must(t, err)
	must(t, permissions.SetGrant(ctx, core.DatabaseGrant{GroupID: g.ID, DatabaseID: target.ID, Permission: core.PermissionView}))
	if p, err := permissions.EffectivePermission(ctx, u.ID, target.ID); err != nil || p != core.PermissionView {
		t.Fatal(p, err)
	}
	second, err := groups.Create(ctx, core.Group{Name: "developers"})
	must(t, err)
	must(t, groups.AddMember(ctx, second.ID, u.ID))
	must(t, permissions.SetGrant(ctx, core.DatabaseGrant{GroupID: second.ID, DatabaseID: target.ID, Permission: core.PermissionEdit}))
	if p, err := permissions.EffectivePermission(ctx, u.ID, target.ID); err != nil || p != core.PermissionEdit {
		t.Fatal(p, err)
	}
	if p, err := permissions.EffectivePermission(ctx, other.ID, target.ID); err != nil || p != core.PermissionNone {
		t.Fatal(p, err)
	}
	grants, err := permissions.FindAll(ctx)
	must(t, err)
	if len(grants) != 2 {
		t.Fatal(grants)
	}
	must(t, permissions.RemoveGrant(ctx, second.ID, target.ID))
	var revoked bool
	must(t, pool.QueryRow(ctx, "SELECT revoked_at IS NOT NULL FROM sessions WHERE id=$1", s.ID).Scan(&revoked))
	if !revoked {
		t.Fatal("grant revoke must invalidate all sessions")
	}
	must(t, pool.QueryRow(ctx, "SELECT revoked_at IS NOT NULL FROM sessions WHERE id=$1", unrelated.ID).Scan(&revoked))
	if revoked {
		t.Fatal("unrelated user revoked")
	}
	s = newSession(t, pool, u.ID)
	must(t, groups.RemoveMember(ctx, g.ID, u.ID))
	must(t, pool.QueryRow(ctx, "SELECT revoked_at IS NOT NULL FROM sessions WHERE id=$1", s.ID).Scan(&revoked))
	if !revoked {
		t.Fatal("membership removal must invalidate sessions")
	}
	if err := groups.RemoveMember(ctx, g.ID, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := groups.FindMembers(ctx, 99999); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	if err := permissions.SetGrant(ctx, core.DatabaseGrant{GroupID: g.ID, DatabaseID: 99999, Permission: core.PermissionEdit}); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	if err := permissions.RemoveGrant(ctx, g.ID, 99999); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestTargetCredentialsAndDisable(t *testing.T) {
	pool := testutil.Platform(t)
	ctx := context.Background()
	key := bytes.Repeat([]byte{2}, 32)
	repo, err := store.NewTargetRepository(pool, key)
	must(t, err)
	record := core.DatabaseTarget{Name: "sales", Driver: "postgres", Host: "127.0.0.1", Port: 54329, Database: "sales", Username: "target_user", SSLMode: "disable", Active: true, Password: "do-not-leak-password"}
	created, err := repo.Create(ctx, record)
	must(t, err)
	var encrypted []byte
	must(t, pool.QueryRow(ctx, "SELECT password_ciphertext FROM database_targets WHERE id=$1", created.ID).Scan(&encrypted))
	if bytes.Contains(encrypted, []byte(record.Password)) || created.Password != "" {
		t.Fatal("password not protected")
	}
	read, err := repo.FindByID(ctx, created.ID)
	must(t, err)
	if read.Password != record.Password {
		t.Fatal("password not decrypted for connector")
	}
	created.Name = "renamed"
	updated, err := repo.Update(ctx, created)
	must(t, err)
	read, err = repo.FindByID(ctx, created.ID)
	must(t, err)
	if updated.Password != "" || read.Password != record.Password {
		t.Fatal("empty update must preserve credential")
	}
	wrongKey, _ := store.NewTargetRepository(pool, bytes.Repeat([]byte{3}, 32))
	if _, err := wrongKey.FindByID(ctx, created.ID); err == nil || strings.Contains(err.Error(), record.Password) {
		t.Fatal("wrong key must fail safely")
	}
	must(t, repo.Disable(ctx, created.ID))
	read, err = repo.FindByID(ctx, created.ID)
	must(t, err)
	if read.Active {
		t.Fatal("target not disabled")
	}
	if err := repo.Disable(ctx, 99999); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	must(t, store.SetupPlatform(ctx, pool))
}

func TestRegistryConfigurationValidation(t *testing.T) {
	if _, err := store.NewTargetRepository(nil, []byte("short")); err == nil {
		t.Fatal("weak key accepted")
	}
	valid := core.DatabaseTarget{Name: "sales", Driver: "postgres", Host: "localhost", Port: 5432, Database: "sales", Username: "target", SSLMode: "verify-full"}
	if err := store.ValidateTarget(valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*core.DatabaseTarget){func(x *core.DatabaseTarget) { x.Driver = "unknown" }, func(x *core.DatabaseTarget) { x.Port = 0 }, func(x *core.DatabaseTarget) { x.SSLMode = "invalid" }} {
		invalid := valid
		change(&invalid)
		if err := store.ValidateTarget(invalid); err == nil {
			t.Fatal("invalid target accepted")
		}
	}
}

func TestAccessMutationRollsBackWhenSessionUpdateFails(t *testing.T) {
	pool := testutil.Platform(t)
	ctx := context.Background()
	groups := store.NewGroupRepository(pool)
	u := user(t, pool, "member")
	g, err := groups.Create(ctx, core.Group{Name: "group"})
	must(t, err)
	must(t, groups.AddMember(ctx, g.ID, u.ID))
	newSession(t, pool, u.ID)
	_, err = pool.Exec(ctx, `CREATE FUNCTION reject_session_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test session update failure'; END $$;
CREATE TRIGGER reject_session_update BEFORE UPDATE ON sessions FOR EACH ROW EXECUTE FUNCTION reject_session_update()`)
	must(t, err)
	if err := groups.RemoveMember(ctx, g.ID, u.ID); err == nil {
		t.Fatal("session update failure ignored")
	}
	members, err := groups.FindMembers(ctx, g.ID)
	must(t, err)
	if len(members) != 1 {
		t.Fatal("membership deletion was not rolled back")
	}
}

func TestGrantDowngradeAndTargetDisableRevokeSessions(t *testing.T) {
	pool := testutil.Platform(t)
	ctx := context.Background()
	groups := store.NewGroupRepository(pool)
	permissions := store.NewPermissionRepository(pool)
	u := user(t, pool, "member")
	g, err := groups.Create(ctx, core.Group{Name: "developers"})
	must(t, err)
	must(t, groups.AddMember(ctx, g.ID, u.ID))
	registry, err := store.NewTargetRepository(pool, bytes.Repeat([]byte{8}, 32))
	must(t, err)
	target, err := registry.Create(ctx, core.DatabaseTarget{Name: "sales", Driver: "postgres", Host: "localhost", Port: 5432, Database: "sales", Username: "target", SSLMode: "disable", Active: true, Password: "password"})
	must(t, err)
	must(t, permissions.SetGrant(ctx, core.DatabaseGrant{GroupID: g.ID, DatabaseID: target.ID, Permission: core.PermissionEdit}))
	s := newSession(t, pool, u.ID)
	must(t, permissions.SetGrant(ctx, core.DatabaseGrant{GroupID: g.ID, DatabaseID: target.ID, Permission: core.PermissionView}))
	var revoked bool
	must(t, pool.QueryRow(ctx, "SELECT revoked_at IS NOT NULL FROM sessions WHERE id=$1", s.ID).Scan(&revoked))
	if !revoked {
		t.Fatal("downgrade must revoke active sessions")
	}
	s = newSession(t, pool, u.ID)
	must(t, registry.Disable(ctx, target.ID))
	must(t, pool.QueryRow(ctx, "SELECT revoked_at IS NOT NULL FROM sessions WHERE id=$1", s.ID).Scan(&revoked))
	if !revoked {
		t.Fatal("disabled target must revoke affected sessions")
	}
}
