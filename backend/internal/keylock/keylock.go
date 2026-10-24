// Package keylock is a mutex per key whose waiters can give up.
package keylock

import (
	"context"
	"sync"
)

// Locker is a mutex per key. An entry lives only while someone holds or waits for its key, so
// the map does not grow with the number of keys used, and a waiter gives up when its context ends.
// The zero value is ready to use.
type Locker struct {
	mu      sync.Mutex
	entries map[string]*keyedEntry
}

type keyedEntry struct {
	sem  chan struct{} // holds one token while the key is locked
	refs int           // holders and waiters
}

// Lock waits for the lock of key and returns its unlock, or ctx's error when ctx ends first.
func (k *Locker) Lock(ctx context.Context, key string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e := k.acquire(key)
	select {
	case e.sem <- struct{}{}:
		return k.unlocker(key, e), nil
	case <-ctx.Done():
		k.release(key, e)
		return nil, ctx.Err()
	}
}

// TryLock takes the lock of key when nobody holds it and returns its unlock; otherwise it does
// not wait and reports false.
func (k *Locker) TryLock(key string) (unlock func(), ok bool) {
	e := k.acquire(key)
	select {
	case e.sem <- struct{}{}:
		return k.unlocker(key, e), true
	default:
		k.release(key, e)
		return nil, false
	}
}

// Busy reports whether any key is held or waited for.
func (k *Locker) Busy() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.entries) > 0
}

// acquire registers the caller as a holder or waiter of key, creating its entry when it has none.
func (k *Locker) acquire(key string) *keyedEntry {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.entries == nil {
		k.entries = make(map[string]*keyedEntry)
	}
	e := k.entries[key]
	if e == nil {
		e = &keyedEntry{sem: make(chan struct{}, 1)}
		k.entries[key] = e
	}
	e.refs++
	return e
}

func (k *Locker) unlocker(key string, e *keyedEntry) func() {
	return func() {
		<-e.sem
		k.release(key, e)
	}
}

func (k *Locker) release(key string, e *keyedEntry) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if e.refs--; e.refs == 0 {
		delete(k.entries, key)
	}
}
