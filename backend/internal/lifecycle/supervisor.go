package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrSupervisorStopping = errors.New("lifecycle supervisor is stopping")
	ErrComponentStopped   = errors.New("lifecycle component stopped unexpectedly")
)

type ComponentError struct {
	Name string
	Err  error
}

func (e ComponentError) Error() string {
	return fmt.Sprintf("lifecycle component %s: %v", e.Name, e.Err)
}

func (e ComponentError) Unwrap() error {
	return e.Err
}

type Supervisor struct {
	ctx      context.Context
	cancel   context.CancelFunc
	failures chan error
	done     chan struct{}
	stopOnce sync.Once
	waitOnce sync.Once
	mu       sync.Mutex
	stopping bool
	started  map[string]struct{}
	failure  error
	wg       sync.WaitGroup
}

func NewSupervisor(parent context.Context) *Supervisor {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &Supervisor{
		ctx:      ctx,
		cancel:   cancel,
		failures: make(chan error, 1),
		done:     make(chan struct{}),
		started:  make(map[string]struct{}),
	}
}

func (s *Supervisor) Start(name string, run func(context.Context) error) error {
	if s == nil || run == nil || name == "" {
		return errors.New("lifecycle component is not configured")
	}
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return ErrSupervisorStopping
	}
	if _, exists := s.started[name]; exists {
		s.mu.Unlock()
		return fmt.Errorf("lifecycle component %q already started", name)
	}
	s.started[name] = struct{}{}
	s.wg.Add(1)
	s.mu.Unlock()

	started := make(chan struct{})
	go func() {
		defer s.wg.Done()
		close(started)
		componentErr := runComponent(s.ctx, run)
		if !s.isStopping() {
			if componentErr == nil {
				componentErr = ErrComponentStopped
			}
			s.reportFailure(ComponentError{Name: name, Err: componentErr})
		}
	}()
	<-started
	return nil
}

func (s *Supervisor) isStopping() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopping
}

func runComponent(ctx context.Context, run func(context.Context) error) (componentErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			componentErr = fmt.Errorf("panic: %v", recovered)
		}
	}()
	return run(ctx)
}

func (s *Supervisor) reportFailure(err error) {
	s.mu.Lock()
	s.failure = err
	s.mu.Unlock()
	select {
	case s.failures <- err:
	default:
	}
}

func (s *Supervisor) LastFailure() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failure
}

func (s *Supervisor) Failures() <-chan error {
	if s == nil {
		return nil
	}
	return s.failures
}

func (s *Supervisor) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		s.BeginStop()
		s.cancel()
		s.waitOnce.Do(func() {
			go func() {
				s.wg.Wait()
				close(s.done)
			}()
		})
	})
}

// BeginStop marks subsequent component exits as expected without canceling the
// component context. This lets listeners drain before periodic work is stopped.
func (s *Supervisor) BeginStop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopping = true
	s.mu.Unlock()
}

func (s *Supervisor) Wait(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.Stop()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
