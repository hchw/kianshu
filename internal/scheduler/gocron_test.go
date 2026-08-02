package scheduler

import (
	"testing"
	"time"
)

func TestGocronValidateCron(t *testing.T) {
	g, err := NewGocron()
	if err != nil {
		t.Fatalf("new gocron: %v", err)
	}
	defer g.Stop()

	if err := g.ValidateCron("0 3 * * *"); err != nil {
		t.Fatalf("valid cron rejected: %v", err)
	}
	if err := g.ValidateCron("not-a-cron"); err == nil {
		t.Fatal("invalid cron accepted")
	}
}

func TestGocronPauseSkipsExecution(t *testing.T) {
	g, err := NewGocron()
	if err != nil {
		t.Fatalf("new gocron: %v", err)
	}
	defer g.Stop()

	count := 0
	jobID, err := g.Schedule("* * * * *", func() { count++ })
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	g.Start()

	// The job runs when not paused.
	if err := g.runNow(jobID); err != nil {
		t.Fatalf("run now: %v", err)
	}
	waitFor(t, &count, 1)

	// Pause prevents execution.
	if err := g.Pause(jobID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := g.runNow(jobID); err != nil {
		t.Fatalf("run now while paused: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if count != 1 {
		t.Fatalf("paused job should not execute, count=%d", count)
	}

	// Resume allows execution again.
	if err := g.Resume(jobID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := g.runNow(jobID); err != nil {
		t.Fatalf("run now after resume: %v", err)
	}
	waitFor(t, &count, 2)
}

// waitFor polls until *c reaches want (RunNow completes asynchronously).
func waitFor(t *testing.T, c *int, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if *c == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("did not reach count %d, got %d", want, *c)
}

func TestGocronRemoveUnknownJob(t *testing.T) {
	g, err := NewGocron()
	if err != nil {
		t.Fatalf("new gocron: %v", err)
	}
	defer g.Stop()
	if err := g.Remove("nope"); err != ErrJobNotFound {
		t.Fatalf("expected ErrJobNotFound, got %v", err)
	}
}
