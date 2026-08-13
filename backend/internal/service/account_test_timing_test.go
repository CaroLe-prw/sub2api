package service

import (
	"testing"
	"time"
)

func TestAccountTestTimingTrackerRecordsFirstContentOnly(t *testing.T) {
	startedAt := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	tracker := &accountTestTimingTracker{startedAt: startedAt}

	tracker.observe(TestEvent{Type: "test_start"}, startedAt.Add(100*time.Millisecond))
	tracker.observe(TestEvent{Type: "status", Text: "waiting"}, startedAt.Add(200*time.Millisecond))
	tracker.observe(TestEvent{Type: "content", Text: "  "}, startedAt.Add(300*time.Millisecond))
	if got := tracker.value(); got != nil {
		t.Fatalf("non-content events must not set TTFT, got %dms", *got)
	}

	tracker.observe(TestEvent{Type: "content", Text: "hello"}, startedAt.Add(450*time.Millisecond))
	tracker.observe(TestEvent{Type: "content", Text: "world"}, startedAt.Add(900*time.Millisecond))

	got := tracker.value()
	if got == nil || *got != 450 {
		t.Fatalf("TTFT = %v, want 450ms", got)
	}
}

func TestAccountTestTimingTrackerClampsNegativeDuration(t *testing.T) {
	startedAt := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	tracker := &accountTestTimingTracker{startedAt: startedAt}

	tracker.observe(TestEvent{Type: "content", Text: "hello"}, startedAt.Add(-time.Millisecond))

	got := tracker.value()
	if got == nil || *got != 0 {
		t.Fatalf("TTFT = %v, want 0ms", got)
	}
}
