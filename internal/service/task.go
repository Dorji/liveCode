package service

import (
	"context"

	"taskmanager/internal/cache"
	"taskmanager/internal/domain"
	"taskmanager/internal/store"
)

type TaskService struct {
	store *store.Store
	cache *cache.TaskCache
}

func NewTaskService(st *store.Store, c *cache.TaskCache) *TaskService {
	return &TaskService{store: st, cache: c}
}

type CreateTaskInput struct {
	TeamID      int64  `json:"team_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	AssigneeID  *int64 `json:"assignee_id"`
}

// Create creates a task in a team. Any member may create; the creator is the
// current user. The assignee, if set, must belong to the same team.
func (ts *TaskService) Create(ctx context.Context, callerID int64, in CreateTaskInput) (domain.Task, error) {
	if in.Title == "" {
		return domain.Task{}, domain.ErrBadRequest
	}
	if _, err := ts.memberRole(ctx, in.TeamID, callerID); err != nil {
		return domain.Task{}, err
	}
	if in.AssigneeID != nil {
		if err := ts.requireMember(ctx, in.TeamID, *in.AssigneeID); err != nil {
			return domain.Task{}, err
		}
	}

	task, err := ts.store.CreateTaskWithHistory(ctx, store.CreateTaskParams{
		TeamID:      in.TeamID,
		Title:       in.Title,
		Description: in.Description,
		CreatedBy:   callerID,
		AssigneeID:  in.AssigneeID,
	})
	if err != nil {
		return domain.Task{}, err
	}
	ts.cache.Invalidate(ctx, in.TeamID)
	return task, nil
}

type UpdateTaskInput struct {
	Title       *string            `json:"title"`
	Description *string            `json:"description"`
	Status      *domain.TaskStatus `json:"status"`
	AssigneeID  *int64             `json:"assignee_id"`
}

// Update applies an update respecting the role-based editing rules:
//   - owner/admin: any field;
//   - creator: title, description, status, assignee (assignee must be in team);
//   - assignee: status only;
//   - everyone else: forbidden.
func (ts *TaskService) Update(ctx context.Context, callerID, taskID int64, in UpdateTaskInput) (domain.Task, error) {
	task, err := ts.store.GetTask(ctx, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	role, err := ts.memberRole(ctx, task.TeamID, callerID)
	if err != nil {
		return domain.Task{}, err
	}

	isAssignee := task.AssigneeID != nil && *task.AssigneeID == callerID
	scope := editScope(role, task.CreatedBy == callerID, isAssignee)

	upd := store.TaskUpdate{}
	switch {
	case scope.All || scope.Creator:
		upd.Title = in.Title
		upd.Description = in.Description
		upd.Status = in.Status
		if in.AssigneeID != nil {
			if err := ts.requireMember(ctx, task.TeamID, *in.AssigneeID); err != nil {
				return domain.Task{}, err
			}
			upd.AssigneeID = in.AssigneeID
		}
	case scope.StatusOnly:
		if in.Title != nil || in.Description != nil || in.AssigneeID != nil {
			return domain.Task{}, domain.ErrForbidden
		}
		upd.Status = in.Status
	default:
		return domain.Task{}, domain.ErrForbidden
	}

	if upd.Status != nil && !upd.Status.Valid() {
		return domain.Task{}, domain.ErrBadRequest
	}

	updated, _, err := ts.store.UpdateTaskWithHistory(ctx, taskID, task.Version, upd, callerID)
	if err != nil {
		return domain.Task{}, err
	}
	ts.cache.Invalidate(ctx, task.TeamID)
	return updated, nil
}

// TaskListResult is a page of tasks plus the cursor for the next page.
type TaskListResult struct {
	Tasks      []domain.Task `json:"tasks"`
	NextCursor *int64        `json:"next_cursor,omitempty"`
}

// List returns the team's tasks with filters and cursor pagination. A Redis
// cache is consulted and populated using a key that includes the team,
// filters, cursor and limit.
func (ts *TaskService) List(ctx context.Context, callerID int64, teamID int64, status *domain.TaskStatus, assigneeID *int64, cursor *int64, limit int) (TaskListResult, error) {
	if _, err := ts.memberRole(ctx, teamID, callerID); err != nil {
		return TaskListResult{}, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	if tasks, next, ok := ts.cache.Get(ctx, teamID, status, assigneeID, cursor, limit); ok {
		return TaskListResult{Tasks: tasks, NextCursor: next}, nil
	}

	page, err := ts.store.ListTasks(ctx, teamID, status, assigneeID, cursor, limit)
	if err != nil {
		return TaskListResult{}, err
	}
	ts.cache.Set(ctx, teamID, status, assigneeID, cursor, limit, page.Tasks, page.NextCursor)
	return TaskListResult{Tasks: page.Tasks, NextCursor: page.NextCursor}, nil
}

func (ts *TaskService) History(ctx context.Context, callerID, taskID int64) ([]domain.TaskHistory, error) {
	task, err := ts.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if _, err := ts.memberRole(ctx, task.TeamID, callerID); err != nil {
		return nil, err
	}
	return ts.store.ListHistory(ctx, taskID)
}

type AddCommentInput struct {
	Content string `json:"content"`
}

func (ts *TaskService) AddComment(ctx context.Context, callerID, taskID int64, in AddCommentInput) (domain.TaskComment, error) {
	if in.Content == "" {
		return domain.TaskComment{}, domain.ErrBadRequest
	}
	task, err := ts.store.GetTask(ctx, taskID)
	if err != nil {
		return domain.TaskComment{}, err
	}
	if _, err := ts.memberRole(ctx, task.TeamID, callerID); err != nil {
		return domain.TaskComment{}, err
	}
	return ts.store.AddComment(ctx, taskID, callerID, in.Content)
}

func (ts *TaskService) Comments(ctx context.Context, callerID, taskID int64) ([]domain.TaskComment, error) {
	task, err := ts.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if _, err := ts.memberRole(ctx, task.TeamID, callerID); err != nil {
		return nil, err
	}
	return ts.store.ListComments(ctx, taskID)
}

// memberRole returns the caller's role in a team or domain.ErrForbidden when
// they are not a member.
func (ts *TaskService) memberRole(ctx context.Context, teamID, userID int64) (domain.Role, error) {
	m, err := ts.store.GetMember(ctx, teamID, userID)
	if err != nil {
		if err == domain.ErrNotFound {
			return "", domain.ErrForbidden
		}
		return "", err
	}
	return m.Role, nil
}

// requireMember ensures userID is a member of teamID; otherwise returns
// domain.ErrAssigneeNotInTeam (used for assignee validation).
func (ts *TaskService) requireMember(ctx context.Context, teamID, userID int64) error {
	if _, err := ts.store.GetMember(ctx, teamID, userID); err != nil {
		if err == domain.ErrNotFound {
			return domain.ErrAssigneeNotInTeam
		}
		return err
	}
	return nil
}
