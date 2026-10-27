// Package service implements business logic: authorization rules, validation
// and orchestration of store/cache calls. It contains no HTTP or SQL.
package service

import "taskmanager/internal/domain"

// EditScope describes which fields a user may change on a task.
type EditScope struct {
	All        bool // owner/admin: title, description, status, assignee
	Creator    bool // creator: title, description, status, assignee
	StatusOnly bool // assignee: status only
}

// editScope derives the task-editing permissions from the member role and the
// relationship to the task. Pure function -> easy to unit test.
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
