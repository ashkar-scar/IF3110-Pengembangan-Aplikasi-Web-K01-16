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

const SESSION_LIFETIME = 8 * time.Hour
const ROTATION_INTERVAL = 15 * time.Minute
const IDLE_TIMEOUT = 30 * time.Minute

var ErrInvalidCredentials = errors.New("Username atau password salah")
var ErrInvalidSession = errors.New("Session tidak valid")

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
		return LoginResult{}, ErrInvalidCredentials
	}

	user, err := service.users.FindByUsername(ctx, username) // Cari user berdasarkan username
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LoginResult{}, ErrInvalidCredentials
		}

		return LoginResult{}, fmt.Errorf("Gagal mengambil data user: %w", err)
	}

	// Akun tidak aktif
	if !user.Active || !PasswordMatches(user.PasswordHash, password) {
		return LoginResult{}, ErrInvalidCredentials
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

// Authenticate memeriksa token dan return  pemilik session
func (service *Service) Authenticate(ctx context.Context, sessionToken string) (core.User, core.Session, error) {
	// Tolak request yang ga membawa token
	if sessionToken == "" {
		return core.User{}, core.Session{}, ErrInvalidSession
	}

	now := time.Now().UTC()
	tokenHash := HashToken(sessionToken)

	// Cari session menggunakan hash token
	session, err := service.sessions.FindByTokenHash(ctx, tokenHash, now)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return core.User{}, core.Session{}, ErrInvalidSession
		}

		return core.User{}, core.Session{}, fmt.Errorf("Gagal mengambil session: %w", err)
	}

	// Tolak session yang revoked, idle, atau absolute expired
	if !session.Active(now, IDLE_TIMEOUT) {
		return core.User{}, core.Session{}, ErrInvalidSession
	}

	// Ambil user yang menjadi pemilik sessionn
	user, err := service.users.FindByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return core.User{}, core.Session{}, ErrInvalidSession
		}

		return core.User{}, core.Session{}, fmt.Errorf("Gagal mengambil pemilik session: %w", err)
	}

	// Tolak session jika akun sudah tidak aktif
	if !user.Active {
		return core.User{}, core.Session{}, ErrInvalidSession
	}

	// Perbarui waktu terakhir session digunakan
	if err := service.sessions.UpdateLastSeen(ctx, session.ID, now); err != nil {
		return core.User{}, core.Session{}, fmt.Errorf("Gagal memperbarui aktivitas session: %w", err)
	}

	session.LastSeenAt = now

	return user, session, nil
}
