package core

import "time"

// Mnyimpan data group user untuk access controll
type Group struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	LeaderID       *int64    `json:"leader_id"`
	LeaderUsername string    `json:"leader_username,omitempty"` // omitempty buat biar username ga ditampilih kalau group blom punya leader
	CreatedAt      time.Time `json:"created_at"`
}