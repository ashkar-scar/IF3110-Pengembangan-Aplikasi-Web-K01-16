package store_test

import (
	"context"
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/testutil"
)

// TestGroupLeader memastikan anggota group dapat ditetapkan sebagai leader
func TestGroupLeader(t *testing.T) {
	pool := testutil.Platform(t)
	ctx := context.Background()
	groups := store.NewGroupRepository(pool)
	member := user(t, pool, "leader")
	group, err := groups.Create(ctx, core.Group{Name: "backend"})

	must(t, err)
	must(t, groups.AddMember(ctx, group.ID, member.ID))

	updated, err := groups.SetLeader(ctx, group.ID, member.ID)
	must(t, err)
	if updated.LeaderID == nil || *updated.LeaderID != member.ID {
		t.Fatalf("leader yang tersimpan tidak sesuai: %+v", updated)
	}
}