package auth

import (
	"context"
	"errors"
	"fmt"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/jackc/pgx/v5"
	"time"
)

const SESSION_LIFETIME = 8 * time.Hour
const ROTATION_INTERVAL = 15 * time.Minute
const IDLE_TIMEOUT = 30 * time.Minute
const GRACE_PERIOD = 30 * time.Second

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
	SessionToken string `json:"-"`
	CSRFToken    string
	ExpiresAt    time.Time
}

// Menyimpan hasil pemeriksaan session
type AuthenticationResult struct {
	User            core.User
	Session         core.Session `json:"-"`
	NewSessionToken string       `json:"-"`
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

// Logout mencabut session berdasarkan token cookie.
func (service *Service) Logout(ctx context.Context, sessionToken string) error {
	// Tolak logout kalau token tidak ada
	if sessionToken == "" {
		return ErrInvalidSession
	}

	now := time.Now().UTC()
	tokenHash := HashToken(sessionToken)

	// Cari session menggunakan hash token
	session, err := service.sessions.FindByTokenHash(ctx, tokenHash, now)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidSession
		}

		return fmt.Errorf("Gagal mengambil session: %w", err)
	}

	// Isi revoked_at agar session tidak bisa dipakai lagi
	if err := service.sessions.Revoke(ctx, session.ID, now); err != nil {
		return fmt.Errorf("Gagal mencabut session: %w", err)
	}

	return nil
}

// Authenticate memeriksa token dan return pemilik session
func (service *Service) Authenticate(ctx context.Context, sessionToken string) (AuthenticationResult, error) {
	// Tolak request yang ga membawa token
	if sessionToken == "" {
		return AuthenticationResult{}, ErrInvalidSession
	}

	now := time.Now().UTC()
	tokenHash := HashToken(sessionToken)

	// Cari session menggunakan hash token
	session, err := service.sessions.FindByTokenHash(ctx, tokenHash, now)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AuthenticationResult{}, ErrInvalidSession
		}

		return AuthenticationResult{}, fmt.Errorf("Gagal mengambil session: %w", err)
	}

	// Tolak session yang revoked, idle, atau absolute expired
	if !session.Active(now, IDLE_TIMEOUT) {
		return AuthenticationResult{}, ErrInvalidSession
	}

	// Ambil user yang menjadi pemilik sessionn
	user, err := service.users.FindByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AuthenticationResult{}, ErrInvalidSession
		}

		return AuthenticationResult{}, fmt.Errorf("Gagal mengambil pemilik session: %w", err)
	}

	// Tolak session jika akun sudah tidak aktif
	if !user.Active {
		return AuthenticationResult{}, ErrInvalidSession
	}

	// Perbarui waktu terakhir session digunakan
	if err := service.sessions.UpdateLastSeen(ctx, session.ID, now); err != nil {
		return AuthenticationResult{}, fmt.Errorf("Gagal memperbarui aktivitas session: %w", err)
	}

	session.LastSeenAt = now

	result := AuthenticationResult{
		User:    user,
		Session: session,
	}

	// Rotasi token secara berkala atau setelah privilege dinaikkan
	shouldRotate := session.RotationRequired || !now.Before(session.RotateAfter)
	if !shouldRotate {
		return result, nil
	}

	// Buat token baru untuk menggantikan token yang sedang digunakan
	newSessionToken, err := GenerateToken()
	if err != nil {
		return AuthenticationResult{}, err
	}

	graceUntil := now.Add(GRACE_PERIOD)
	nextRotation := now.Add(ROTATION_INTERVAL)

	// Simpan token baru dan beri grace period kepada token lama
	err = service.sessions.RotateToken(ctx, session.ID, tokenHash, HashToken(newSessionToken), now, graceUntil, nextRotation)

	if err != nil {
		// Request lain mungkin sudah merotasi token yang sama
		if errors.Is(err, store.ErrSessionTokenNotCurrent) {
			return result, nil
		}
		
		if errors.Is(err, store.ErrInvalidSessionForRotation) {
			return AuthenticationResult{}, ErrInvalidSession
		}

		return AuthenticationResult{}, fmt.Errorf("Gagal merotasi session: %w", err)
	}

	result.Session.RotateAfter = nextRotation
	result.Session.RotationRequired = false
	result.NewSessionToken = newSessionToken

	return result, nil
}
