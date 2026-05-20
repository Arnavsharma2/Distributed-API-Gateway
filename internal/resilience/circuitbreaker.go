package resilience

import (
	"sync"
	"time"

	"github.com/aps/gatekeeper/internal/config"
)

type State string

const (
	StateClosed   State = "closed"
	StateOpen     State = "open"
	StateHalfOpen State = "half_open"
)

type CircuitBreaker struct {
	mu               sync.Mutex
	failureThreshold int
	cooldown         time.Duration
	state            State
	failures         int
	openedAt         time.Time
}

type Registry struct {
	breakers map[string]*CircuitBreaker
}

func NewCircuitBreaker(failureThreshold int, cooldown time.Duration) *CircuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if cooldown <= 0 {
		cooldown = 20 * time.Second
	}
	return &CircuitBreaker{
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
		state:            StateClosed,
	}
}

func NewRegistry(routes []config.RouteConfig) *Registry {
	breakers := map[string]*CircuitBreaker{}
	for _, route := range routes {
		if route.CircuitBreaker.Enabled {
			breakers[route.Name] = NewCircuitBreaker(
				route.CircuitBreaker.FailureThreshold,
				time.Duration(route.CircuitBreaker.CooldownSeconds)*time.Second,
			)
		}
	}
	return &Registry{breakers: breakers}
}

func (r *Registry) Get(routeName string) (*CircuitBreaker, bool) {
	breaker, ok := r.breakers[routeName]
	return breaker, ok
}

func (b *CircuitBreaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state != StateOpen {
		return true
	}
	if time.Since(b.openedAt) >= b.cooldown {
		b.state = StateHalfOpen
		return true
	}
	return false
}

func (b *CircuitBreaker) OnSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures = 0
	b.state = StateClosed
}

func (b *CircuitBreaker) OnFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures++
	if b.state == StateHalfOpen || b.failures >= b.failureThreshold {
		b.state = StateOpen
		b.openedAt = time.Now()
	}
}

func (b *CircuitBreaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}
