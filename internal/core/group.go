package core

import "time"

// Mnyimpan data group user untuk access controll
type Group struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}