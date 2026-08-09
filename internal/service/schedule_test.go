package service

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/scheduler"
)

// fakeScheduler implements the scheduler plugin interface in memory, recording
// each fn so a test can fire it like a cron tick.
type fakeScheduler struct {
	mu      sync.Mutex
	jobs    map[string]func()
	next    int
	fired   int
	removed []string
	paused  []string
}

func newFakeScheduler() *fakeScheduler {
	return &fakeScheduler{jobs: map[string]func(){}}
}

func (f *fakeScheduler) Start()      {}
func (f *fakeScheduler) Stop()       {}
func (f *fakeScheduler) ValidateCron(expr string) error {
	if expr != "not-a-cron" {
		return nil
	}
	return fmt.Errorf("invalid cron %q", expr)
}
func (f *fakeScheduler) Schedule(_ string, fn func()) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	handle := fmt.Sprintf("job-%d", f.next)
	f.next++
	f.jobs[handle] = fn
	return handle, nil
}
func (f *fakeScheduler) Remove(jobID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.jobs, jobID)
	f.removed = append(f.removed, jobID)
	return nil
}
func (f *fakeScheduler) Pause(jobID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = append(f.paused, jobID)
	return nil
}
func (f *fakeScheduler) Resume(jobID string) error { return nil }

// fire runs every registered job once, simulating cron ticks.
func (f *fakeScheduler) fire() {
	f.mu.Lock()
	fns := make([]func(), 0, len(f.jobs))
	for _, fn := range f.jobs {
		fns = append(fns, fn)
	}
	f.mu.Unlock()
	for _, fn := range fns {
		fn()
		f.fired++
	}
}

func TestScheduleCreateListPauseResumeDelete(t *testing.T) {
	gdb := testDB(t)
	mgr := NewScheduleManager(gdb, newFakeScheduler(), 30*time.Second, nil)

	ts := &model.TestSet{Name: "ts", OwnerID: 1}
	if err := gdb.Create(ts).Error; err != nil {
		t.Fatalf("create test set: %v", err)
	}
	f, err := CreateFlow(gdb, ts.ID, 1, "flow")
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	s, err := mgr.CreateSchedule(f.ID, "0 3 * * *")
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	if !s.Enabled || s.JobID == "" {
		t.Fatalf("new schedule should be enabled with a job, got %+v", s)
	}

	list, err := mgr.ListSchedules(f.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != s.ID {
		t.Fatalf("expected 1 schedule, got %+v", list)
	}

	paused, err := mgr.SetScheduleEnabled(f.ID, s.ID, false)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if paused.Enabled {
		t.Fatal("schedule should be disabled after pause")
	}
	resumed, err := mgr.SetScheduleEnabled(f.ID, s.ID, true)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !resumed.Enabled {
		t.Fatal("schedule should be enabled after resume")
	}

	updated, err := mgr.UpdateSchedule(f.ID, s.ID, "0 4 * * *")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Cron != "0 4 * * *" {
		t.Fatalf("cron not updated: %+v", updated)
	}

	if err := mgr.DeleteSchedule(f.ID, s.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := mgr.GetSchedule(f.ID, s.ID); err != ErrScheduleNotFound {
		t.Fatalf("expected ErrScheduleNotFound after delete, got %v", err)
	}
}

func TestScheduleInvalidCronRejected(t *testing.T) {
	gdb := testDB(t)
	mgr := NewScheduleManager(gdb, newFakeScheduler(), 30*time.Second, nil)
	ts := &model.TestSet{Name: "ts", OwnerID: 1}
	if err := gdb.Create(ts).Error; err != nil {
		t.Fatalf("create test set: %v", err)
	}
	f, err := CreateFlow(gdb, ts.ID, 1, "flow")
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	if _, err := mgr.CreateSchedule(f.ID, "not-a-cron"); err == nil {
		t.Fatal("expected invalid cron to be rejected")
	}
}

func TestScheduleTriggerRunsEnabledVersion(t *testing.T) {
	gdb := testDB(t)
	backend := newFakeScheduler()
	mgr := NewScheduleManager(gdb, backend, 30*time.Second, nil)

	flowID, _ := setupFlow(t, gdb, "http://example.com")
	v, _, err := SaveAndEnable(gdb, flowID, 1)
	if err != nil {
		t.Fatalf("save and enable: %v", err)
	}

	s, err := mgr.CreateSchedule(flowID, "0 3 * * *")
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	// Simulate cron firing.
	backend.fire()

	logs, _, err := ListRuns(gdb, flowID, 1, 20)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 run log after trigger, got %d", len(logs))
	}
	if logs[0].VersionID != v.ID || logs[0].VersionNo != v.VersionNo {
		t.Fatalf("scheduled run should execute enabled version %d/%d, got %d/%d",
			v.ID, v.VersionNo, logs[0].VersionID, logs[0].VersionNo)
	}
	if s.JobID == "" {
		t.Fatal("schedule should have a job handle")
	}
}

func TestScheduleTriggerSkipsPaused(t *testing.T) {
	gdb := testDB(t)
	backend := newFakeScheduler()
	mgr := NewScheduleManager(gdb, backend, 30*time.Second, nil)

	flowID, _ := setupFlow(t, gdb, "http://example.com")
	if _, _, err := SaveAndEnable(gdb, flowID, 1); err != nil {
		t.Fatalf("save and enable: %v", err)
	}
	s, err := mgr.CreateSchedule(flowID, "0 3 * * *")
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	if _, err := mgr.SetScheduleEnabled(flowID, s.ID, false); err != nil {
		t.Fatalf("pause: %v", err)
	}

	backend.fire()

	logs, _, err := ListRuns(gdb, flowID, 1, 20)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("paused schedule should not run, got %d logs", len(logs))
	}
}

var _ scheduler.Scheduler = (*fakeScheduler)(nil)
