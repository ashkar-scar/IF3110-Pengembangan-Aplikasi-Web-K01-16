package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/testutil"
)

func TestLoginRotationGraceAndRevocation(t *testing.T) {
	pool := testutil.Platform(t)
	ctx := context.Background()
	users := store.NewUserRepository(pool)
	hash, err := auth.CreatePasswordHash("testing-password")
	if err != nil {
		t.Fatal(err)
	}
	user, err := users.Create(ctx, core.User{Username: "member", PasswordHash: hash, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	service := auth.NewService(users, store.NewSessionRepository(pool))
	for _, input := range [][2]string{{"missing", "testing-password"}, {"member", "wrong"}, {"", ""}} {
		if _, err := service.Login(ctx, input[0], input[1]); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatal(err)
		}
	}
	login, err := service.Login(ctx, user.Username, "testing-password")
	if err != nil {
		t.Fatal(err)
	}
	initial, err := service.Authenticate(ctx, login.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	absolute := initial.Session.AbsoluteExpiresAt
	if _, err := pool.Exec(ctx, "UPDATE sessions SET rotate_after=NOW()-INTERVAL '1 minute' WHERE id=$1", initial.Session.ID); err != nil {
		t.Fatal(err)
	}
	const requests = 6
	results := make([]auth.AuthenticationResult, requests)
	errorsFound := make([]error, requests)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i], errorsFound[i] = service.Authenticate(ctx, login.SessionToken) }()
	}
	wg.Wait()
	var newToken string
	rotations := 0
	for i, result := range results {
		if errorsFound[i] != nil {
			t.Fatal(errorsFound[i])
		}
		if result.NewSessionToken != "" {
			newToken = result.NewSessionToken
			rotations++
		}
		if !result.Session.AbsoluteExpiresAt.Equal(absolute) {
			t.Fatal("rotation extended absolute expiry")
		}
	}
	if rotations != 1 {
		t.Fatalf("expected one atomic rotation, got %d", rotations)
	}
	if _, err := service.Authenticate(ctx, newToken); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, login.SessionToken); err != nil {
		t.Fatal("old token must work during grace", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE session_tokens SET grace_until=NOW()-INTERVAL '1 second' WHERE token_hash=$1", auth.HashToken(login.SessionToken)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, login.SessionToken); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatal("expired grace accepted", err)
	}
	if err := service.Logout(ctx, newToken); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, newToken); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatal("revoked session accepted", err)
	}
	if _, err := service.Authenticate(ctx, "random-token"); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatal(err)
	}
	if err := service.Logout(ctx, ""); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatal(err)
	}
	var stored []byte
	if err := pool.QueryRow(ctx, "SELECT token_hash FROM session_tokens WHERE token_hash=$1", auth.HashToken(newToken)).Scan(&stored); err != nil || string(stored) == newToken {
		t.Fatal("raw session token stored", err)
	}
}

func TestSessionIdleAbsoluteAndInactiveAccount(t *testing.T) {
	pool := testutil.Platform(t)
	ctx := context.Background()
	users := store.NewUserRepository(pool)
	hash, err := auth.CreatePasswordHash("testing-password")
	if err != nil {
		t.Fatal(err)
	}
	u, err := users.Create(ctx, core.User{Username: "member", PasswordHash: hash, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	service := auth.NewService(users, store.NewSessionRepository(pool))
	for _, change := range []string{"last_seen_at=NOW()-INTERVAL '31 minutes'", "absolute_expires_at=NOW()-INTERVAL '1 second'"} {
		login, err := service.Login(ctx, u.Username, "testing-password")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "UPDATE sessions SET "+change+" WHERE user_id=$1", u.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Authenticate(ctx, login.SessionToken); !errors.Is(err, auth.ErrInvalidSession) {
			t.Fatal(change, err)
		}
	}
	login, err := service.Login(ctx, u.Username, "testing-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_active=FALSE WHERE id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, login.SessionToken); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatal(err)
	}
	if _, err := service.Login(ctx, u.Username, "testing-password"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatal(err)
	}
	if login.ExpiresAt.Sub(time.Now()) > auth.SESSION_LIFETIME {
		t.Fatal("invalid absolute expiry")
	}
}
