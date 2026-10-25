package core

import (
	"testing"
	"time"
)

func TestPermissionAndAdminPolicy(t *testing.T) {
	if HighestPermission(PermissionView, PermissionEdit, PermissionNone) != PermissionEdit || HighestPermission() != PermissionNone {
		t.Fatal("effective permission must use the highest grant")
	}
	if PermissionView.Can(PermissionEdit) || !PermissionEdit.Can(PermissionView) || PermissionNone.Can(PermissionView) {
		t.Fatal("invalid permission ordering")
	}
	for _, value := range []string{"NONE", "OWNER", ""} {
		if _, err := ParsePermission(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	if parsed, err := ParsePermission(" edit "); err != nil || parsed != PermissionEdit {
		t.Fatal(parsed, err)
	}
	if (User{PlatformAdmin: true}).CanManagePlatform() || (User{Active: true}).CanManagePlatform() || !(User{Active: true, PlatformAdmin: true}).CanManagePlatform() {
		t.Fatal("admin requires an active account")
	}
}

func TestSessionExpiryBoundaries(t *testing.T) {
	now := time.Now()
	session := Session{LastSeenAt: now.Add(-30 * time.Minute), AbsoluteExpiresAt: now.Add(time.Hour)}
	if !session.Active(now, 30*time.Minute) || session.Active(now.Add(time.Nanosecond), 30*time.Minute) {
		t.Fatal("idle boundary is incorrect")
	}
	session.LastSeenAt = now
	session.AbsoluteExpiresAt = now
	if session.Active(now, 30*time.Minute) {
		t.Fatal("absolute expiry must reject an otherwise active session")
	}
	session.AbsoluteExpiresAt = now.Add(time.Hour)
	session.RevokedAt = &now
	if session.Active(now, 30*time.Minute) {
		t.Fatal("revoked session accepted")
	}
}
