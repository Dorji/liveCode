package service

import "taskmanager/internal/domain"

type EditScope struct {
	All        bool // owner/admin: title, description, status, assignee
	Creator    bool // creator: title, description, status, assignee
	StatusOnly bool // assignee: status only
}

func editScope(role domain.Role, isCreator, isAssignee bool) EditScope {
	switch {
	case role == domain.RoleOwner || role == domain.RoleAdmin:
		return EditScope{All: true}
	case isCreator:
		return EditScope{Creator: true}
	case isAssignee:
		return EditScope{StatusOnly: true}
	default:
		return EditScope{}
	}
}

func canInvite(role domain.Role) bool {
	return role == domain.RoleOwner || role == domain.RoleAdmin
}

func canViewStats(role domain.Role) bool {
	return role == domain.RoleOwner || role == domain.RoleAdmin
}
