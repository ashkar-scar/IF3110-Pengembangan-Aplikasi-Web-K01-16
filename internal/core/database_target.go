package core

type DatabaseTarget struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Driver   string `json:"driver"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	SSLMode  string `json:"sslmode"`
	Active   bool   `json:"is_active"`
	Password string `json:"-"`
}
