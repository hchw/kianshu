package service

import "sync"

// AgentEventBus keeps in-memory event buffers for active agent runs so that a
// client that reconnects (e.g. after a page refresh) can pick up events it
// missed and continue receiving new ones in real time.
type AgentEventBus struct {
	mu    sync.Mutex
	flows map[uint]*flowRun
}

type flowRun struct {
	mu     sync.Mutex
	cond   *sync.Cond
	events []Event
	done   bool
}

// NewAgentEventBus creates a new event bus.
func NewAgentEventBus() *AgentEventBus {
	return &AgentEventBus{flows: make(map[uint]*flowRun)}
}

func (b *AgentEventBus) getOrCreate(flowID uint) *flowRun {
	b.mu.Lock()
	defer b.mu.Unlock()
	fr, ok := b.flows[flowID]
	if !ok {
		fr = &flowRun{}
		fr.cond = sync.NewCond(&fr.mu)
		b.flows[flowID] = fr
	}
	return fr
}

// Push appends an event to the flow's buffer and wakes subscribers.
func (b *AgentEventBus) Push(flowID uint, ev Event) {
	fr := b.getOrCreate(flowID)
	fr.mu.Lock()
	fr.events = append(fr.events, ev)
	fr.cond.Broadcast()
	fr.mu.Unlock()
}

// MarkDone signals that the run has finished.
func (b *AgentEventBus) MarkDone(flowID uint) {
	fr := b.getOrCreate(flowID)
	fr.mu.Lock()
	fr.done = true
	fr.cond.Broadcast()
	fr.mu.Unlock()
}

// Remove cleans up the flow's event buffer.
func (b *AgentEventBus) Remove(flowID uint) {
	b.mu.Lock()
	delete(b.flows, flowID)
	b.mu.Unlock()
}

// Subscribe sends all events since the given index, then blocks for new events
// and sends them in batches. It returns when the run is done or the stop
// channel is closed.
func (b *AgentEventBus) Subscribe(flowID uint, since int, emit func([]Event), stop <-chan struct{}) {
	fr := b.getOrCreate(flowID)
	fr.mu.Lock()
	defer fr.mu.Unlock()

	for {
		if since < len(fr.events) {
			batch := make([]Event, len(fr.events)-since)
			copy(batch, fr.events[since:])
			since = len(fr.events)
			// Emit outside the lock to avoid deadlocks.
			fr.mu.Unlock()
			emit(batch)
			fr.mu.Lock()
			continue
		}
		if fr.done {
			return
		}
		// Wait with timeout-aware select; cond.Wait can't be selected on, so
		// we use a goroutine to wake us.
		doneCh := make(chan struct{})
		go func() {
			select {
			case <-stop:
			case <-doneCh:
			}
			fr.cond.Broadcast() // wake the waiter
		}()
		fr.cond.Wait()
		close(doneCh)
		// After waking, check if stop was the reason.
		select {
		case <-stop:
			return
		default:
		}
	}
}
