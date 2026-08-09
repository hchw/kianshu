package service

import (
	"sync"

	"github/hchw/kianshu/internal/model"
)

// RunEventBus broadcasts newly created execution logs to subscribers so the
// frontend can update the run list in real time without polling or full-page
// refreshes.
type RunEventBus struct {
	mu   sync.RWMutex
	subs map[uint]map[chan *model.ExecutionLog]struct{}
}

// NewRunEventBus creates a new run event bus.
func NewRunEventBus() *RunEventBus {
	return &RunEventBus{subs: make(map[uint]map[chan *model.ExecutionLog]struct{})}
}

// Publish pushes a newly created execution log to all subscribers of its flow.
func (b *RunEventBus) Publish(log *model.ExecutionLog) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs[log.FlowID] {
		select {
		case ch <- log:
		default:
		}
	}
}

// Subscribe returns a buffered channel that receives execution logs for the
// given flow. Callers must call Unsubscribe when done.
func (b *RunEventBus) Subscribe(flowID uint) chan *model.ExecutionLog {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan *model.ExecutionLog, 32)
	if b.subs[flowID] == nil {
		b.subs[flowID] = make(map[chan *model.ExecutionLog]struct{})
	}
	b.subs[flowID][ch] = struct{}{}
	return ch
}

// Unsubscribe removes the subscription and closes the channel.
func (b *RunEventBus) Unsubscribe(flowID uint, ch chan *model.ExecutionLog) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[flowID]; ok {
		delete(b.subs[flowID], ch)
		if len(b.subs[flowID]) == 0 {
			delete(b.subs, flowID)
		}
	}
	close(ch)
}
