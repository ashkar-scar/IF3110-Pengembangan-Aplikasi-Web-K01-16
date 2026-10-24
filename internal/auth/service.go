package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/jackc/pgx/v5"
)

// Define some constants
const SESSION_LIFETIME = 8 * time.Hour
const ROTATION_INTERVAL = 15 * time.Minute

// Service -> menjalankan proses autentikasi
type Service struct {
	users    *store.UserRepository
	sessions *store.SessionRepository
}

// Menyimpan hasil login yang berhasil
type LoginResult struct {
	User         core.User
	SessionToken string
	CSRFToken    string
	ExpiresAt    time.Time
}

// NewService membuat service autentikasi
func NewService(users *store.UserRepository, sessions *store.SessionRepository) *Service {
	return &Service{
		users:    users,
		sessions: sessions,
	}
}

// Meriksa akun dan membuat session baru
func (service *Service) Login(ctx context.Context, username string, password string) (LoginResult, error) {
	if username == "" || password == "" { // Tolak kalau kosong
		return LoginResult{}, errors.New("Username atau password salah")
	}

	user, err := service.users.FindByUsername(ctx, username) // Cari user berdasarkan username
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LoginResult{}, errors.New("Username atau password salah")
		}

		return LoginResult{}, fmt.Errorf("Gagal mengambil data user: %w", err)
	}

	// Akun tidak aktif
	if !user.Active || !PasswordMatches(user.PasswordHash, password) {
		return LoginResult{}, errors.New("Username atau password salah")
	}

	// Bikin token acak yg dikirim lewat cookie
	sessionToken, err := GenerateToken()
	if err != nil {
		return LoginResult{}, err
	}

	// Token buat CSRF
	csrfToken, err := GenerateToken()
	if err != nil {
		return LoginResult{}, err
	}

	now := time.Now().UTC()

	session := core.Session{
		UserID:            user.ID,
		CSRFToken:         csrfToken,
		CreatedAt:         now,
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(SESSION_LIFETIME),
		RotateAfter:       now.Add(ROTATION_INTERVAL),
		RotationRequired:  false,
	}

	// SImpan session
	session, err = service.sessions.Create(ctx, session, HashToken(sessionToken))

	if err != nil {
		return LoginResult{}, fmt.Errorf("Gagal membuat session: %w", err)
	}

	return LoginResult{
		User:         user,
		SessionToken: sessionToken,
		CSRFToken:    csrfToken,
		ExpiresAt:    session.AbsoluteExpiresAt,
	}, nil
}