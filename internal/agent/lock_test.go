package agent

import "testing"

func TestKeyedLockSerializesPerKey(t *testing.T) {
	locks := NewKeyedLock()
	unlock := locks.TryLock(1)
	if unlock == nil || locks.TryLock(1) != nil {
		t.Fatal("same key should be busy")
	}
	if locks.TryLock(2) == nil {
		t.Fatal("different key should not be blocked")
	}
	unlock()
	unlock2 := locks.TryLock(1)
	if unlock2 == nil {
		t.Fatal("key should be available after unlock")
	}
	unlock2()
}
