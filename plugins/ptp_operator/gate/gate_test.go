package gate

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWait_ReturnsImmediatelyWhenNeverReset(t *testing.T) {
	var g Gate
	start := time.Now()
	g.Wait(5 * time.Second)
	assert.Less(t, time.Since(start), 100*time.Millisecond)
}

func TestOpen_BeforeReset_DoesNotPanic(t *testing.T) {
	var g Gate
	assert.NotPanics(t, func() { g.Open() })
}

func TestResetOpen_UnblocksWaiters(t *testing.T) {
	var g Gate
	g.Reset()

	done := make(chan struct{})
	go func() {
		g.Wait(5 * time.Second)
		close(done)
	}()

	// Waiter should be blocked
	select {
	case <-done:
		t.Fatal("Wait returned before Open was called")
	case <-time.After(50 * time.Millisecond):
	}

	g.Open()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait did not return after Open")
	}
}

func TestWait_TimesOut(t *testing.T) {
	var g Gate
	g.Reset()

	start := time.Now()
	g.Wait(100 * time.Millisecond)
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestOpen_MultipleCallsSafe(t *testing.T) {
	var g Gate
	g.Reset()

	assert.NotPanics(t, func() {
		for i := 0; i < 10; i++ {
			g.Open()
		}
	})
}

func TestReset_ConcurrentResetIsNoop(t *testing.T) {
	var g Gate

	// Hold the gate in Reset state by running a slow operation:
	// first Reset acquires the write lock, second should be a no-op via TryLock.
	g.Reset()

	// While the gate is reset (not yet opened), a second Reset should
	// return immediately without blocking.
	done := make(chan struct{})
	go func() {
		g.Reset()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent Reset blocked instead of being a no-op")
	}

	g.Open()
}

func TestResetOpen_MultipleWaiters(t *testing.T) {
	var g Gate
	g.Reset()

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			g.Wait(5 * time.Second)
		}()
	}

	time.Sleep(50 * time.Millisecond)
	g.Open()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("not all waiters unblocked after Open")
	}
}

func TestGate_ResetAfterOpen_BlocksAgain(t *testing.T) {
	var g Gate

	// First cycle
	g.Reset()
	g.Open()

	// Second cycle — should block again
	g.Reset()

	done := make(chan struct{})
	go func() {
		g.Wait(5 * time.Second)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Wait returned before second Open")
	case <-time.After(50 * time.Millisecond):
	}

	g.Open()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait did not return after second Open")
	}
}

func TestGate_ConcurrentWaitAndOpen(t *testing.T) {
	var g Gate

	// Stress test: many goroutines waiting while Open fires concurrently
	const cycles = 50
	for i := 0; i < cycles; i++ {
		g.Reset()

		var waiters atomic.Int32
		var wg sync.WaitGroup
		wg.Add(3)
		for j := 0; j < 3; j++ {
			go func() {
				defer wg.Done()
				g.Wait(time.Second)
				waiters.Add(1)
			}()
		}

		time.Sleep(5 * time.Millisecond)
		g.Open()
		wg.Wait()

		assert.Equal(t, int32(3), waiters.Load(), "cycle %d: not all waiters completed", i)
	}
}
