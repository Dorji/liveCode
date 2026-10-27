package service

import (
	"testing"

	"taskmanager/internal/domain"
)

func TestEditScope(t *testing.T) {
	tests := []struct {
		name       string
		role       domain.Role
		isCreator  bool
		isAssignee bool
		want       EditScope
	}{
		{"owner", domain.RoleOwner, false, false, EditScope{All: true}},
		{"admin", domain.RoleAdmin, false, false, EditScope{All: true}},
		{"creator", domain.RoleMember, true, false, EditScope{Creator: true}},
		{"assignee", domain.RoleMember, false, true, EditScope{StatusOnly: true}},
		{"member-unrelated", domain.RoleMember, false, false, EditScope{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := editScope(tt.role, tt.isCreator, tt.isAssignee); got != tt.want {
				t.Fatalf("editScope() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRoleHelpers(t *testing.T) {
	if !canInvite(domain.RoleOwner) || !canInvite(domain.RoleAdmin) {
		t.Fatal("owner/admin must be able to invite")
	}
	if canInvite(domain.RoleMember) {
		t.Fatal("member must not be able to invite")
	}
	if !canViewStats(domain.RoleOwner) || !canViewStats(domain.RoleAdmin) {
		t.Fatal("owner/admin must be able to view stats")
	}
	if canViewStats(domain.RoleMember) {
		t.Fatal("member must not be able to view stats")
	}
}
