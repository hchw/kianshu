package agent

import "sync"

// KeyedLock serializes one Agent target per key without coupling the runtime
// to a particular domain model.
type KeyedLock struct {
	mu   sync.Mutex
	held map[uint]*sync.Mutex
}

func NewKeyedLock() *KeyedLock { return &KeyedLock{held: map[uint]*sync.Mutex{}} }

// TryLock returns an unlock function, or nil when the target is busy.
func (l *KeyedLock) TryLock(key uint) func() {
	l.mu.Lock()
	m := l.held[key]
	if m == nil {
		m = &sync.Mutex{}
		l.held[key] = m
	}
	l.mu.Unlock()
	if !m.TryLock() {
		return nil
	}
	return m.Unlock
}
