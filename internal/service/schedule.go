package service

import (
	"errors"
	"time"

	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/scheduler"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// ErrScheduleNotFound indicates a missing scheduled task.
var ErrScheduleNotFound = errors.New("调度任务不存在")

// ErrInvalidCron indicates a malformed cron expression.
var ErrInvalidCron = errors.New("cron 表达式无效")

// ScheduleManager owns the scheduled-task registry and drives a Scheduler
// backend. The backend is the plugin seam: swap GocronScheduler for an external
// implementation without changing schedule behavior.
type ScheduleManager struct {
	db          *gorm.DB
	scheduler   scheduler.Scheduler
	execTimeout time.Duration
	runBus      *RunEventBus
}

// NewScheduleManager builds a manager over the given backend. execTimeout
// bounds each scheduled run so a hung job cannot occupy the slot forever.
func NewScheduleManager(db *gorm.DB, sch scheduler.Scheduler, execTimeout time.Duration, runBus *RunEventBus) *ScheduleManager {
	return &ScheduleManager{db: db, scheduler: sch, execTimeout: execTimeout, runBus: runBus}
}

// Start registers all enabled schedules and starts the backend.
func (m *ScheduleManager) Start() {
	var schedules []model.FlowSchedule
	if err := m.db.Where("enabled = ?", true).Find(&schedules).Error; err != nil {
		log.Errorf("调度:加载已启用任务失败: %v", err)
	}
	for _, s := range schedules {
		m.register(&s)
	}
	m.scheduler.Start()
}

// Stop stops the backend.
func (m *ScheduleManager) Stop() {
	m.scheduler.Stop()
}

// register wires one schedule into the backend. Invalid cron rows are skipped
// with a logged error so one bad row cannot take the scheduler down.
func (m *ScheduleManager) register(s *model.FlowSchedule) {
	jobID, err := m.scheduler.Schedule(s.Cron, m.trigger(s.ID))
	if err != nil {
		log.Errorf("调度:注册任务 %d (cron=%q) 失败: %v", s.ID, s.Cron, err)
		return
	}
	s.JobID = jobID
	if err := m.db.Model(s).Update("job_id", jobID).Error; err != nil {
		log.Errorf("调度:更新任务 %d job_id 失败: %v", s.ID, err)
	}
}

// trigger returns the function executed each time a schedule fires: it runs the
// flow's currently enabled version and records an execution log.
func (m *ScheduleManager) trigger(scheduleID uint) func() {
	return func() {
		log.Infof("调度:触发任务 %d", scheduleID)
		var s model.FlowSchedule
		if err := m.db.First(&s, scheduleID).Error; err != nil {
			log.Warnf("调度:任务 %d 不存在,跳过", scheduleID)
			return
		}
		if !s.Enabled {
			log.Infof("调度:任务 %d 已停用,跳过", scheduleID)
			return
		}
		var v model.FlowVersion
		if err := m.db.Where("flow_id = ? AND enabled = ?", s.FlowID, true).First(&v).Error; err != nil {
			log.Warnf("调度:任务 %d (flow=%d) 无启用版本,跳过", scheduleID, s.FlowID)
			return
		}
		log.Infof("调度:开始执行任务 %d, flow=%d, version=%d", scheduleID, s.FlowID, v.VersionNo)
		result, err := RunVersion(m.db, s.FlowID, v.VersionNo, m.execTimeout)
		if err != nil {
			log.Errorf("调度:任务 %d 执行失败: %v", scheduleID, err)
		} else {
			log.Infof("调度:任务 %d 执行成功, run_id=%d, status=%s", scheduleID, result.ID, result.Status)
			if m.runBus != nil {
				m.runBus.Publish(result)
			}
		}
	}
}

// CreateSchedule adds a scheduled task for a flow and enables it in the backend.
func (m *ScheduleManager) CreateSchedule(flowID uint, cron string) (*model.FlowSchedule, error) {
	if err := m.scheduler.ValidateCron(cron); err != nil {
		return nil, ErrInvalidCron
	}
	var f model.TestFlow
	if err := m.db.First(&f, flowID).Error; err != nil {
		return nil, ErrFlowNotFound
	}
	s := &model.FlowSchedule{
		FlowID:    flowID,
		TestSetID: f.TestSetID,
		Cron:      cron,
		Enabled:   true,
	}
	if err := m.db.Create(s).Error; err != nil {
		return nil, err
	}
	m.register(s)
	return s, nil
}

// ListSchedules returns the schedules of a flow, newest first.
func (m *ScheduleManager) ListSchedules(flowID uint) ([]model.FlowSchedule, error) {
	var schedules []model.FlowSchedule
	if err := m.db.Where("flow_id = ?", flowID).Order("id desc").Find(&schedules).Error; err != nil {
		return nil, err
	}
	return schedules, nil
}

// UpdateSchedule changes a schedule's cron expression.
func (m *ScheduleManager) UpdateSchedule(flowID, scheduleID uint, cron string) (*model.FlowSchedule, error) {
	if err := m.scheduler.ValidateCron(cron); err != nil {
		return nil, ErrInvalidCron
	}
	s, err := m.GetSchedule(flowID, scheduleID)
	if err != nil {
		return nil, err
	}
	s.Cron = cron
	if err := m.db.Save(s).Error; err != nil {
		return nil, err
	}
	m.reload(s)
	return s, nil
}

// SetScheduleEnabled pauses (false) or resumes (true) a schedule.
func (m *ScheduleManager) SetScheduleEnabled(flowID, scheduleID uint, enabled bool) (*model.FlowSchedule, error) {
	s, err := m.GetSchedule(flowID, scheduleID)
	if err != nil {
		return nil, err
	}
	s.Enabled = enabled
	if err := m.db.Save(s).Error; err != nil {
		return nil, err
	}
	m.reload(s)
	return s, nil
}

// DeleteSchedule removes a schedule and its backend job.
func (m *ScheduleManager) DeleteSchedule(flowID, scheduleID uint) error {
	s, err := m.GetSchedule(flowID, scheduleID)
	if err != nil {
		return err
	}
	if s.JobID != "" {
		if err := m.scheduler.Remove(s.JobID); err != nil && !errors.Is(err, scheduler.ErrJobNotFound) {
			log.Errorf("调度:移除任务 %d 失败: %v", scheduleID, err)
		}
	}
	return m.db.Delete(&model.FlowSchedule{}, scheduleID).Error
}

// GetSchedule returns a schedule scoped to its flow.
func (m *ScheduleManager) GetSchedule(flowID, scheduleID uint) (*model.FlowSchedule, error) {
	var s model.FlowSchedule
	if err := m.db.Where("id = ? AND flow_id = ?", scheduleID, flowID).First(&s).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrScheduleNotFound
		}
		return nil, err
	}
	return &s, nil
}

// reload reapplies a schedule to the backend after a DB change.
func (m *ScheduleManager) reload(s *model.FlowSchedule) {
	if s.JobID != "" {
		if err := m.scheduler.Remove(s.JobID); err != nil && !errors.Is(err, scheduler.ErrJobNotFound) {
			log.Errorf("调度:移除任务 %d 失败: %v", s.ID, err)
		}
		s.JobID = ""
	}
	if s.Enabled {
		m.register(s)
	}
}
