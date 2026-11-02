package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"taskmanager/internal/domain"
)

type TaskCache struct {
	rdb     *redis.Client
	ttl     time.Duration
	breaker *CircuitBreaker
}

type Option func(*TaskCache)

func WithCircuitBreaker(threshold int, cooldown time.Duration) Option {
	return func(c *TaskCache) {
		c.breaker = NewCircuitBreaker(threshold, cooldown)
	}
}

func New(addr string, ttl time.Duration, opts ...Option) *TaskCache {
	c := &TaskCache{
		rdb:     redis.NewClient(&redis.Options{Addr: addr}),
		ttl:     ttl,
		breaker: NewCircuitBreaker(3, 30*time.Second),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *TaskCache) BreakerState() State { return c.breaker.State() }

func (c *TaskCache) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

func genKey(teamID int64) string {
	return fmt.Sprintf("tasks:gen:%d", teamID)
}

// Invalidate bumps the team generation counter so stale entries are never read again.
func (c *TaskCache) Invalidate(ctx context.Context, teamID int64) {
	if !c.breaker.Allow() {
		return
	}
	if err := c.rdb.Incr(ctx, genKey(teamID)).Err(); err != nil {
		c.fail(err)
		return
	}
	c.breaker.Success()
}

func (c *TaskCache) generation(ctx context.Context, teamID int64) (int64, error) {
	v, err := c.rdb.Get(ctx, genKey(teamID)).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return v, err
}

func cacheKey(teamID, gen int64, status *domain.TaskStatus, assigneeID *int64, cursor *int64, limit int) string {
	s, a, c := "-", "-", "-"
	if status != nil {
		s = string(*status)
	}
	if assigneeID != nil {
		a = strconv.FormatInt(*assigneeID, 10)
	}
	if cursor != nil {
		c = strconv.FormatInt(*cursor, 10)
	}
	return fmt.Sprintf("tasks:list:%d:%d:%s:%s:%s:%d", teamID, gen, s, a, c, limit)
}

type cachedPage struct {
	Tasks      []domain.Task `json:"tasks"`
	NextCursor *int64        `json:"next_cursor"`
}

func (c *TaskCache) Get(ctx context.Context, teamID int64, status *domain.TaskStatus, assigneeID *int64, cursor *int64, limit int) ([]domain.Task, *int64, bool) {
	if !c.breaker.Allow() {
		return nil, nil, false
	}
	gen, err := c.generation(ctx, teamID)
	if err != nil {
		c.fail(err)
		return nil, nil, false
	}
	data, err := c.rdb.Get(ctx, cacheKey(teamID, gen, status, assigneeID, cursor, limit)).Bytes()
	if err != nil {
		if err == redis.Nil {
			c.breaker.Success() // miss is not a failure
		} else {
			c.fail(err)
		}
		return nil, nil, false
	}
	c.breaker.Success()
	var p cachedPage
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, nil, false
	}
	return p.Tasks, p.NextCursor, true
}

func (c *TaskCache) Set(ctx context.Context, teamID int64, status *domain.TaskStatus, assigneeID *int64, cursor *int64, limit int, tasks []domain.Task, nextCursor *int64) {
	if !c.breaker.Allow() {
		return
	}
	gen, err := c.generation(ctx, teamID)
	if err != nil {
		c.fail(err)
		return
	}
	data, err := json.Marshal(cachedPage{Tasks: tasks, NextCursor: nextCursor})
	if err != nil {
		return
	}
	if err := c.rdb.Set(ctx, cacheKey(teamID, gen, status, assigneeID, cursor, limit), data, c.ttl).Err(); err != nil {
		c.fail(err)
		return
	}
	c.breaker.Success()
}

func (c *TaskCache) fail(err error) {
	if c.breaker.Failure() {
		slog.Warn("redis circuit breaker opened", "error", err)
	}
}
