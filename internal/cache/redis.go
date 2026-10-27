// Package cache implements Redis caching for task lists with graceful
// degradation: if Redis is unavailable, reads fall back to the database and
// writes are skipped.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"taskmanager/internal/domain"
)

type TaskCache struct {
	rdb *redis.Client
	ttl time.Duration
}

func New(addr string, ttl time.Duration) *TaskCache {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	return &TaskCache{rdb: rdb, ttl: ttl}
}

func (c *TaskCache) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

func genKey(teamID int64) string {
	return fmt.Sprintf("tasks:gen:%d", teamID)
}

// Invalidate bumps the per-team generation counter. Cache keys embed the
// generation, so after a bump old entries are simply never requested again and
// expire naturally via TTL — no SCAN/DEL fan-out is needed.
func (c *TaskCache) Invalidate(ctx context.Context, teamID int64) {
	_ = c.rdb.Incr(ctx, genKey(teamID)).Err()
}

func (c *TaskCache) generation(ctx context.Context, teamID int64) (int64, error) {
	v, err := c.rdb.Get(ctx, genKey(teamID)).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return v, err
}

func cacheKey(teamID, gen int64, status *domain.TaskStatus, assigneeID *int64, limit, offset int) string {
	s, a := "-", "-"
	if status != nil {
		s = string(*status)
	}
	if assigneeID != nil {
		a = strconv.FormatInt(*assigneeID, 10)
	}
	return fmt.Sprintf("tasks:list:%d:%d:%s:%s:%d:%d", teamID, gen, s, a, limit, offset)
}

func (c *TaskCache) Get(ctx context.Context, teamID int64, status *domain.TaskStatus, assigneeID *int64, limit, offset int) ([]domain.Task, bool) {
	gen, err := c.generation(ctx, teamID)
	if err != nil {
		return nil, false
	}
	data, err := c.rdb.Get(ctx, cacheKey(teamID, gen, status, assigneeID, limit, offset)).Bytes()
	if err != nil {
		return nil, false
	}
	var tasks []domain.Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, false
	}
	return tasks, true
}

func (c *TaskCache) Set(ctx context.Context, teamID int64, status *domain.TaskStatus, assigneeID *int64, limit, offset int, tasks []domain.Task) {
	gen, err := c.generation(ctx, teamID)
	if err != nil {
		return
	}
	data, err := json.Marshal(tasks)
	if err != nil {
		return
	}
	_ = c.rdb.Set(ctx, cacheKey(teamID, gen, status, assigneeID, limit, offset), data, c.ttl).Err()
}
