package scheduler

// Scheduler is the plugin interface for running cron-triggered jobs. The
// default implementation uses gocron; an external scheduling service can
// implement the same interface and be swapped in without touching flow or run
// behavior.
type Scheduler interface {
	// Start begins processing registered jobs.
	Start()
	// Stop gracefully stops the scheduler and its jobs.
	Stop()
	// Schedule registers a cron job. cron is a standard 5-field expression.
	// It returns a stable job handle used by Remove, Pause, and Resume.
	Schedule(cron string, fn func()) (string, error)
	// ValidateCron reports whether cron is a valid standard 5-field expression.
	ValidateCron(cron string) error
	// Remove unregisters a job by handle.
	Remove(jobID string) error
	// Pause stops a job from executing without unregistering it.
	Pause(jobID string) error
	// Resume allows a paused job to execute again.
	Resume(jobID string) error
}
