package logx

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestLimiterAllowsFirstFiveThenEveryThousandth(t *testing.T) {
	var l Limiter
	var allowed []int
	for i := 1; i <= 2500; i++ {
		if l.Allow() {
			allowed = append(allowed, i)
		}
	}
	want := []int{1, 2, 3, 4, 5, 1000, 2000}
	if len(allowed) != len(want) {
		t.Fatalf("allowed calls %v, want %v", allowed, want)
	}
	for i := range want {
		if allowed[i] != want[i] {
			t.Fatalf("allowed calls %v, want %v", allowed, want)
		}
	}
}

func TestLimiterNextReportsRunningCount(t *testing.T) {
	var l Limiter
	for i := 1; i <= 5; i++ {
		if n, ok := l.Next(); !ok || n != uint64(i) {
			t.Fatalf("call %d: n=%d ok=%v", i, n, ok)
		}
	}
	if n, ok := l.Next(); ok || n != 6 {
		t.Fatalf("call 6: n=%d ok=%v, want 6 suppressed", n, ok)
	}
}

func TestLimiterConcurrentUse(t *testing.T) {
	var l Limiter
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				if l.Allow() {
					allowed.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 5+4 {
		t.Fatalf("%d allowed of 4000, want 9 (first 5 plus 1000, 2000, 3000, 4000)", got)
	}
}
