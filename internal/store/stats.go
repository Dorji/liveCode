package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"taskmanager/internal/domain"
)

// TopAssignee is one row of the "top assignees" statistic.
type TopAssignee struct {
	AssigneeID  int64  `json:"assignee_id"`
	Name        string `json:"name"`
	ClosedCount int64  `json:"closed_count"`
}

// TeamStats is the result of the SQL report for a single team.
type TeamStats struct {
	StatusCounts    map[domain.TaskStatus]int64 `json:"status_counts"`
	TopAssignees    []TopAssignee               `json:"top_assignees"`
	AvgCloseSeconds float64                     `json:"avg_close_seconds"`
	TotalComments   int64                       `json:"total_comments"`
}

// statsSQL is a single query with CTEs that returns every metric at once:
//   - team_tasks narrows the working set to one team (access filtering);
//   - status_stats groups by status;
//   - top_assignees joins users, filters closed tasks in the last 30 days and
//     orders by count with LIMIT 3;
//   - avg_close averages the seconds between creation and closure;
//   - comment_stats counts comments on the team's tasks.
//
// JOIN, GROUP BY, aggregates and date functions (NOW, DATE_SUB, TIMESTAMPDIFF)
// are all exercised in one statement — no N+1 queries.
const statsSQL = `
WITH team_tasks AS (
    SELECT id, status, assignee_id, created_at, closed_at
    FROM tasks
    WHERE team_id = ?
),
status_stats AS (
    SELECT status, COUNT(*) AS cnt FROM team_tasks GROUP BY status
),
top_assignees AS (
    SELECT tt.assignee_id, u.name, COUNT(*) AS closed_count
    FROM team_tasks tt
    JOIN users u ON u.id = tt.assignee_id
    WHERE tt.status = 'done'
      AND tt.closed_at >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 30 DAY)
    GROUP BY tt.assignee_id, u.name
    ORDER BY closed_count DESC
    LIMIT 3
),
avg_close AS (
    SELECT AVG(TIMESTAMPDIFF(SECOND, created_at, closed_at)) AS avg_close_seconds
    FROM team_tasks
    WHERE status = 'done' AND closed_at IS NOT NULL
),
comment_stats AS (
    SELECT COUNT(*) AS total_comments
    FROM task_comments tc
    JOIN team_tasks tt ON tt.id = tc.task_id
)
SELECT
    COALESCE((SELECT JSON_OBJECTAGG(status, cnt) FROM status_stats), JSON_OBJECT())        AS status_counts,
    COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('assignee_id', assignee_id, 'name', name,
                'closed_count', closed_count)) FROM top_assignees), JSON_ARRAY())         AS top_assignees,
    COALESCE((SELECT avg_close_seconds FROM avg_close), 0)                                 AS avg_close_seconds,
    COALESCE((SELECT total_comments FROM comment_stats), 0)                                AS total_comments`

func (s *Store) TeamStats(ctx context.Context, teamID int64) (TeamStats, error) {
	var stats TeamStats
	var statusJSON, assigneesJSON []byte
	var avgClose sql.NullFloat64

	err := s.db.QueryRowContext(ctx, statsSQL, teamID).
		Scan(&statusJSON, &assigneesJSON, &avgClose, &stats.TotalComments)
	if err != nil {
		return TeamStats{}, err
	}

	stats.StatusCounts = map[domain.TaskStatus]int64{}
	if len(statusJSON) > 0 {
		var m map[string]int64
		if err := json.Unmarshal(statusJSON, &m); err != nil {
			return TeamStats{}, err
		}
		for k, v := range m {
			stats.StatusCounts[domain.TaskStatus(k)] = v
		}
	}

	stats.TopAssignees = []TopAssignee{}
	if len(assigneesJSON) > 0 {
		if err := json.Unmarshal(assigneesJSON, &stats.TopAssignees); err != nil {
			return TeamStats{}, err
		}
	}

	stats.AvgCloseSeconds = avgClose.Float64
	return stats, nil
}
