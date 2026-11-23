package service

import (
	"os"
	"runtime"
	"testing"
)

// Reproduce unrelated service goroutines allocating while AllocsPerRun samples
// process-wide counters. The child guard also lets this harness verify isolation.
func TestCIThinkingAllocationWithBackgroundWork(t *testing.T) {
	if os.Getenv("SUB2API_THINKING_FILTER_ALLOC_CHILD") != "1" {
		stop := make(chan struct{})
		done := make(chan struct{})
		ready := make(chan struct{})
		go func() {
			defer close(done)
			close(ready)
			for {
				select {
				case <-stop:
					return
				default:
				}
				allocations := make([][]byte, 512)
				for i := range allocations {
					allocations[i] = make([]byte, 128)
				}
				runtime.KeepAlive(allocations)
				runtime.Gosched()
			}
		}()
		<-ready
		t.Cleanup(func() { close(stop); <-done })
	}
	TestFilterThinkingBlocksSignedHistorySkipsDecode(t)
}
