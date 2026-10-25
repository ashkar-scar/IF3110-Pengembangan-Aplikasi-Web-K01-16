package core

// Menyimpan izin group terhadap target database
type DatabaseGrant struct {
	GroupID    int64      `json:"group_id"`
	DatabaseID int64      `json:"database_id"`
	Permission Permission `json:"permission"`
}