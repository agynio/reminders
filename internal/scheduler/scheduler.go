package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type FireFunc func(ctx context.Context, reminderID uuid.UUID)

type Scheduler struct {
	ctx    context.Context
	cancel context.CancelFunc
	fire   FireFunc

	mu     sync.Mutex
	timers map[uuid.UUID]*time.Timer
}

func New(fire FireFunc) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		ctx:    ctx,
		cancel: cancel,
		fire:   fire,
		timers: make(map[uuid.UUID]*time.Timer),
	}
}

func (s *Scheduler) Schedule(id uuid.UUID, at time.Time) {
	select {
	case <-s.ctx.Done():
		return
	default:
	}

	delay := time.Until(at)
	if delay < 0 {
		delay = 0
	}

	s.mu.Lock()
	if existing, ok := s.timers[id]; ok {
		existing.Stop()
	}
	timer := time.AfterFunc(delay, func() {
		s.mu.Lock()
		delete(s.timers, id)
		s.mu.Unlock()

		select {
		case <-s.ctx.Done():
			return
		default:
		}

		s.fire(s.ctx, id)
	})
	s.timers[id] = timer
	s.mu.Unlock()
}

func (s *Scheduler) Cancel(id uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	timer, ok := s.timers[id]
	if !ok {
		return false
	}
	stopped := timer.Stop()
	delete(s.timers, id)
	return stopped
}

func (s *Scheduler) Stop() {
	s.cancel()
	s.mu.Lock()
	for id, timer := range s.timers {
		timer.Stop()
		delete(s.timers, id)
	}
	s.mu.Unlock()
}

func (s *Scheduler) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.timers)
}
