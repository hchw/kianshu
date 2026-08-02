package scheduler

import (
	"errors"
	"fmt"

	"sync"
	"sync/atomic"

	"github.com/go-co-op/gocron/v2"
	"github.com/robfig/cron/v3"
)

// ErrJobNotFound indicates the job handle is unknown.
var ErrJobNotFound = errors.New("调度任务不存在")

// managedJob pairs a gocron job with an atomic pause flag. Paused jobs stay
// registered (so the schedule persists) but skip execution.
type managedJob struct {
	handle string
	job    gocron.Job
	paused atomic.Bool
}

// GocronScheduler is the default Scheduler implementation backed by
// github.com/go-co-op/gocron.
type GocronScheduler struct {
	sched  gocron.Scheduler
	mu     sync.RWMutex
	jobs   map[string]*managedJob
	nextID uint64
}

// NewGocron returns a stopped GocronScheduler. Call Start before schedules fire.
func NewGocron() (*GocronScheduler, error) {
	s, err := gocron.NewScheduler()
	if err != nil {
		return nil, err
	}
	return &GocronScheduler{sched: s, jobs: map[string]*managedJob{}}, nil
}

// Start begins processing registered jobs.
func (g *GocronScheduler) Start() { g.sched.Start() }

// Stop gracefully shuts the scheduler down.
func (g *GocronScheduler) Stop() {
	_ = g.sched.Shutdown()
	g.mu.Lock()
	g.jobs = map[string]*managedJob{}
	g.mu.Unlock()
}

// Schedule registers a cron job and returns a stable handle for it.
func (g *GocronScheduler) Schedule(cron string, fn func()) (string, error) {
	handle := fmt.Sprintf("job-%d", atomic.AddUint64(&g.nextID, 1))
	wrapped := &managedJob{handle: handle}
	job, err := g.sched.NewJob(
		gocron.CronJob(cron, false),
		gocron.NewTask(func() {
			if wrapped.paused.Load() {
				return
			}
			fn()
		}),
	)
	if err != nil {
		return "", err
	}
	wrapped.job = job
	g.mu.Lock()
	g.jobs[handle] = wrapped
	g.mu.Unlock()
	return handle, nil
}

// ValidateCron reports whether cron is a valid standard 5-field expression.
func (g *GocronScheduler) ValidateCron(expr string) error {
	_, err := cron.ParseStandard(expr)
	return err
}

// Remove unregisters a job by handle.
func (g *GocronScheduler) Remove(jobID string) error {
	g.mu.Lock()
	wrapped, ok := g.jobs[jobID]
	if ok {
		delete(g.jobs, jobID)
	}
	g.mu.Unlock()
	if !ok {
		return ErrJobNotFound
	}
	return g.sched.RemoveJob(wrapped.job.ID())
}

// Pause stops a job from executing while keeping it registered.
func (g *GocronScheduler) Pause(jobID string) error {
	g.mu.RLock()
	wrapped, ok := g.jobs[jobID]
	g.mu.RUnlock()
	if !ok {
		return ErrJobNotFound
	}
	wrapped.paused.Store(true)
	return nil
}

// Resume allows a paused job to execute again.
func (g *GocronScheduler) Resume(jobID string) error {
	g.mu.RLock()
	wrapped, ok := g.jobs[jobID]
	g.mu.RUnlock()
	if !ok {
		return ErrJobNotFound
	}
	wrapped.paused.Store(false)
	return nil
}

// runNow fires a registered job once regardless of its cron schedule, for
// deterministic tests of pause/resume behavior. Paused jobs skip execution.
func (g *GocronScheduler) runNow(jobID string) error {
	g.mu.RLock()
	wrapped, ok := g.jobs[jobID]
	g.mu.RUnlock()
	if !ok {
		return ErrJobNotFound
	}
	return wrapped.job.RunNow()
}
