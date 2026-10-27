package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"taskmanager/internal/domain"
)

// TaskUpdate carries optional update fields; nil pointers mean "no change".
type TaskUpdate struct {
	Title       *string
	Description *string
	Status      *domain.TaskStatus
	AssigneeID  *int64
}

type CreateTaskParams struct {
	TeamID      int64
	Title       string
	Description string
	CreatedBy   int64
	AssigneeID  *int64
}

// CreateTaskWithHistory inserts a task and records the initial "creation"
// history entry in a single transaction.
func (s *Store) CreateTaskWithHistory(ctx context.Context, p CreateTaskParams) (domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, version)
		 VALUES (?, ?, ?, ?, ?, ?, 1)`,
		p.TeamID, p.Title, p.Description, domain.StatusTodo, p.CreatedBy, p.AssigneeID)
	if err != nil {
		return domain.Task{}, err
	}
	taskID, err := res.LastInsertId()
	if err != nil {
		return domain.Task{}, err
	}

	changes := []domain.Change{
		{Field: "title", Old: nil, New: p.Title},
		{Field: "description", Old: nil, New: p.Description},
		{Field: "status", Old: nil, New: string(domain.StatusTodo)},
		{Field: "assignee_id", Old: nil, New: p.AssigneeID},
	}
	if err := insertHistory(ctx, tx, taskID, p.CreatedBy, changes); err != nil {
		return domain.Task{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	return s.GetTask(ctx, taskID)
}

// UpdateTaskWithHistory updates a task with optimistic locking (the version
// column) and records old/new values for changed fields atomically. It returns
// domain.ErrConflict when the expected version no longer matches.
func (s *Store) UpdateTaskWithHistory(ctx context.Context, taskID int64, expectedVersion int, upd TaskUpdate, changedBy int64) (domain.Task, []domain.Change, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, nil, err
	}
	defer tx.Rollback()

	old, err := s.getTask(ctx, tx, taskID)
	if err != nil {
		return domain.Task{}, nil, err
	}
	if old.Version != expectedVersion {
		return domain.Task{}, nil, domain.ErrConflict
	}

	newTitle := old.Title
	if upd.Title != nil {
		newTitle = *upd.Title
	}
	newDesc := old.Description
	if upd.Description != nil {
		newDesc = *upd.Description
	}
	newStatus := old.Status
	if upd.Status != nil {
		newStatus = *upd.Status
	}
	newAssignee := old.AssigneeID
	if upd.AssigneeID != nil {
		newAssignee = upd.AssigneeID
	}

	newClosedAt := sql.NullTime{}
	if old.ClosedAt != nil {
		newClosedAt = sql.NullTime{Time: *old.ClosedAt, Valid: true}
	}

	changes := []domain.Change{}
	if newTitle != old.Title {
		changes = append(changes, domain.Change{Field: "title", Old: old.Title, New: newTitle})
	}
	if newDesc != old.Description {
		changes = append(changes, domain.Change{Field: "description", Old: old.Description, New: newDesc})
	}
	if newStatus != old.Status {
		changes = append(changes, domain.Change{Field: "status", Old: string(old.Status), New: string(newStatus)})
		if newStatus == domain.StatusDone {
			newClosedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
		} else {
			newClosedAt = sql.NullTime{}
		}
	}
	if !equalInt64Ptr(old.AssigneeID, newAssignee) {
		changes = append(changes, domain.Change{Field: "assignee_id", Old: old.AssigneeID, New: newAssignee})
	}

	// Nothing actually changed: no history, no version bump.
	if len(changes) == 0 {
		if err := tx.Commit(); err != nil {
			return domain.Task{}, nil, err
		}
		return old, nil, nil
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE tasks
		 SET title = ?, description = ?, status = ?, assignee_id = ?, closed_at = ?, version = version + 1
		 WHERE id = ? AND version = ?`,
		newTitle, newDesc, newStatus, newAssignee, newClosedAt, taskID, expectedVersion)
	if err != nil {
		return domain.Task{}, nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return domain.Task{}, nil, err
	}
	if affected == 0 {
		return domain.Task{}, nil, domain.ErrConflict
	}

	if err := insertHistory(ctx, tx, taskID, changedBy, changes); err != nil {
		return domain.Task{}, nil, err
	}

	if err := tx.Commit(); err != nil {
		return domain.Task{}, nil, err
	}

	updated, err := s.GetTask(ctx, taskID)
	if err != nil {
		return domain.Task{}, nil, err
	}
	return updated, changes, nil
}

func (s *Store) GetTask(ctx context.Context, id int64) (domain.Task, error) {
	return s.getTask(ctx, s.db, id)
}

func (s *Store) getTask(ctx context.Context, q queryer, id int64) (domain.Task, error) {
	row := q.QueryRowContext(ctx,
		`SELECT id, team_id, title, description, status, created_by, assignee_id,
		        created_at, updated_at, closed_at, version
		 FROM tasks WHERE id = ?`, id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Task{}, domain.ErrNotFound
	}
	return t, err
}

// ListTasks returns the team's tasks with optional filters and pagination.
func (s *Store) ListTasks(ctx context.Context, teamID int64, status *domain.TaskStatus, assigneeID *int64, limit, offset int) ([]domain.Task, error) {
	query := `SELECT id, team_id, title, description, status, created_by, assignee_id,
	                 created_at, updated_at, closed_at, version
	          FROM tasks WHERE team_id = ?`
	args := []any{teamID}
	if status != nil {
		query += ` AND status = ?`
		args = append(args, string(*status))
	}
	if assigneeID != nil {
		query += ` AND assignee_id = ?`
		args = append(args, *assigneeID)
	}
	query += ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := []domain.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) ListHistory(ctx context.Context, taskID int64) ([]domain.TaskHistory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, changed_by, changes, created_at
		 FROM task_history WHERE task_id = ? ORDER BY created_at DESC, id DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	history := []domain.TaskHistory{}
	for rows.Next() {
		var h domain.TaskHistory
		var changesJSON []byte
		if err := rows.Scan(&h.ID, &h.TaskID, &h.ChangedBy, &changesJSON, &h.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(changesJSON, &h.Changes); err != nil {
			return nil, err
		}
		history = append(history, h)
	}
	return history, rows.Err()
}

func (s *Store) AddComment(ctx context.Context, taskID, userID int64, content string) (domain.TaskComment, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO task_comments (task_id, user_id, content) VALUES (?, ?, ?)`,
		taskID, userID, content)
	if err != nil {
		return domain.TaskComment{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.TaskComment{}, err
	}

	var c domain.TaskComment
	err = s.db.QueryRowContext(ctx,
		`SELECT id, task_id, user_id, content, created_at FROM task_comments WHERE id = ?`, id).
		Scan(&c.ID, &c.TaskID, &c.UserID, &c.Content, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskComment{}, domain.ErrNotFound
	}
	return c, err
}

func (s *Store) ListComments(ctx context.Context, taskID int64) ([]domain.TaskComment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, user_id, content, created_at
		 FROM task_comments WHERE task_id = ? ORDER BY created_at ASC, id ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	comments := []domain.TaskComment{}
	for rows.Next() {
		var c domain.TaskComment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.UserID, &c.Content, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

func insertHistory(ctx context.Context, tx *sql.Tx, taskID, changedBy int64, changes []domain.Change) error {
	data, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO task_history (task_id, changed_by, changes) VALUES (?, ?, ?)`,
		taskID, changedBy, data)
	return err
}

func scanTask(sc scanner) (domain.Task, error) {
	var t domain.Task
	var status string
	var assignee sql.NullInt64
	var closedAt sql.NullTime
	err := sc.Scan(&t.ID, &t.TeamID, &t.Title, &t.Description, &status, &t.CreatedBy,
		&assignee, &t.CreatedAt, &t.UpdatedAt, &closedAt, &t.Version)
	if err != nil {
		return domain.Task{}, err
	}
	t.Status = domain.TaskStatus(status)
	if assignee.Valid {
		v := assignee.Int64
		t.AssigneeID = &v
	}
	if closedAt.Valid {
		v := closedAt.Time
		t.ClosedAt = &v
	}
	return t, nil
}

func equalInt64Ptr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
