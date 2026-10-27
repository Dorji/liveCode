package main

import (
	"context"
	"log"
	"sync"
	"time"
)

type Semaphore struct {
	ch chan struct{}
}

func New(capacity int) *Semaphore {
	if capacity <= 0 {
		panic("semaphore: capacity must be > 0")
	}
	return &Semaphore{ch: make(chan struct{}, capacity)}
}

// Acquire блокируется до получения слота или отмены контекста.
func (s *Semaphore) Acquire(ctx context.Context) error {
	select {
	case s.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TryAcquire пытается получить слот без блокировки.
// Возвращает true, если слот получен.
func (s *Semaphore) TryAcquire() bool {
	select {
	case s.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

// Release освобождает слот. Паникует, если освобождать нечего —
// это сигнал о баге в вызывающем коде.
func (s *Semaphore) Release() {
	select {
	case <-s.ch:
	default:
		panic("semaphore: release without acquire")
	}
}

// Cap возвращает ёмкость семафора.
func (s *Semaphore) Cap() int { return cap(s.ch) }

// Len возвращает текущее количество занятых слотов.
func (s *Semaphore) Len() int { return len(s.ch) }

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sem := New(3) // не более 3 параллельных задач

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			if err := sem.Acquire(ctx); err != nil {
				log.Printf("task %d: %v", id, err)
				return
			}
			defer sem.Release()

			// полезная работа
			time.Sleep(500 * time.Millisecond)
			log.Printf("task %d done", id)
		}(i)
	}
	wg.Wait()
}
