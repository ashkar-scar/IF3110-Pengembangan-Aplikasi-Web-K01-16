package core

import "time"

// Session menyimpan status login milik user
type Session struct {
	ID                int64
	UserID            int64
	CSRFToken         string // Token buat melindungi request yang mengubah data
	CreatedAt         time.Time
	LastSeenAt        time.Time
	AbsoluteExpiresAt time.Time
	RotateAfter       time.Time // Menentukan kapan token perlu diganti scara rutin
	RevokedAt         *time.Time
	RotationRequired  bool // Menandai bahwa token harus segera diganti
}

// Menentukan apakah session masih dapat digunakan
func (session Session) Active(now time.Time, idleLimit time.Duration) bool {
	if session.RevokedAt != nil {
		return false
	}

	if !now.Before(session.AbsoluteExpiresAt) {
		return false
	}

	return now.Sub(session.LastSeenAt) <= idleLimit
}

// SessionToken menyimpan hash token cookie milik session.
type SessionToken struct {
	SessionID  int64
	TokenHash  []byte
	CreatedAt  time.Time
	GraceUntil *time.Time // Batas waktu token lamam masih diperbolehkan stlh rotasi
}
