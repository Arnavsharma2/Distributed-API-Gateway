package resilience

import (
	"testing"
	"time"
)

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	breaker := NewCircuitBreaker(2, time.Minute)

	if !breaker.Allow() {
		t.Fatal("closed breaker should allow requests")
	}
	breaker.OnFailure()
	if breaker.State() != StateClosed {
		t.Fatalf("expected closed after one failure, got %s", breaker.State())
	}
	breaker.OnFailure()
	if breaker.State() != StateOpen {
		t.Fatalf("expected open after threshold failures, got %s", breaker.State())
	}
	if breaker.Allow() {
		t.Fatal("open breaker should reject during cooldown")
	}
}

func TestCircuitBreakerHalfOpenAfterCooldown(t *testing.T) {
	breaker := NewCircuitBreaker(1, time.Millisecond)
	breaker.OnFailure()
	time.Sleep(2 * time.Millisecond)

	if !breaker.Allow() {
		t.Fatal("breaker should allow trial request after cooldown")
	}
	if breaker.State() != StateHalfOpen {
		t.Fatalf("expected half-open state, got %s", breaker.State())
	}
	breaker.OnSuccess()
	if breaker.State() != StateClosed {
		t.Fatalf("expected closed after successful trial, got %s", breaker.State())
	}
}
