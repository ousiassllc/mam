package service

import (
	"context"
	"sync"
)

// keyedLock is a mutex per key. An entry lives only while someone holds or waits for its key, so
// the map does not grow with the number of keys used, and a waiter gives up when its context ends.
// The zero value is ready to use.
type keyedLock struct {
	mu      sync.Mutex
	entries map[string]*keyedEntry
}

type keyedEntry struct {
	sem  chan struct{} // holds one token while the key is locked
	refs int           // holders and waiters
}

// lock waits for the lock of key and returns its unlock, or ctx's error when ctx ends first.
func (k *keyedLock) lock(ctx context.Context, key string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	if k.entries == nil {
		k.entries = make(map[string]*keyedEntry)
	}
	e := k.entries[key]
	if e == nil {
		e = &keyedEntry{sem: make(chan struct{}, 1)}
		k.entries[key] = e
	}
	e.refs++
	k.mu.Unlock()

	select {
	case e.sem <- struct{}{}:
		return func() {
			<-e.sem
			k.release(key, e)
		}, nil
	case <-ctx.Done():
		k.release(key, e)
		return nil, ctx.Err()
	}
}

func (k *keyedLock) release(key string, e *keyedEntry) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if e.refs--; e.refs == 0 {
		delete(k.entries, key)
	}
}
