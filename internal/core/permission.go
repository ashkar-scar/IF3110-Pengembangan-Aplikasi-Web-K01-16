package core

import (
	"fmt"
	"strings"
)

// Create Permission types
type Permission string

const (
	PermissionNone Permission = "NONE"
	PermissionView Permission = "VIEW"
	PermissionEdit Permission = "EDIT"
)

// Parse permission jadi request permission yang valid
func ParsePermission(raw string) (Permission, error) {
	permission := Permission(strings.ToUpper(strings.TrimSpace(raw)))

	switch permission {
	case PermissionView, PermissionEdit:
		return permission, nil

	default:
		return PermissionNone, fmt.Errorf("Permission %q unrecognized", raw)
	}
}

// Menghitung izin efektif user yang masuk ke beberpaa grup
func HighestPermission(permissions ...Permission) Permission {
	highest := PermissionNone

	for _, permission := range permissions {
		if permissionLevel(permission) > permissionLevel(highest) {
			highest = permission
		}
	}

	return highest
}

// Mmeriksa apakah permission user cukup melakukan operasi tertentu
func (current Permission) Can(required Permission) bool {
	return permissionLevel(current) >= permissionLevel(required)
}

// Get permission level
func permissionLevel(permission Permission) int {
	switch permission {
	case PermissionView:
		return 1
	case PermissionEdit:
		return 2
	default:
		return 0
	}
}
