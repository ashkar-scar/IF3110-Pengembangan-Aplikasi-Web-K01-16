package core

import "time"

// Data user
type User struct {
	ID            int64     `json:"id"`
	Username      string    `json:"username"`
	PasswordHash  string    `json:"-"`
	PlatformAdmin bool      `json:"is_platform_admin"` // Menentukan pakah user itu admin
	Active        bool      `json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
}

// Kasih akses sebagai admin
func (u User) CanManagePlatform() bool {
	return u.Active && u.PlatformAdmin
}