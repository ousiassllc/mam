// Package venhance holds what "make a video larger / smoother" (Issue #1031) decides before any work is done: the plan (the checked values and the
// size and rate of the output), the probe of the source file, and the quotes of the cloud the user has yet to confirm. The contract shared with
// the cloud provider is agent/cloudenhance.
package venhance

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
)

// EstimateTTL is how long a quote of the cloud can be confirmed: the provider's request is kept for it, and a price the user saw long ago is
// not what they confirm now.
const EstimateTTL = 30 * time.Minute

// Quote is a quote of the cloud kept until the user confirms it: the request that was quoted and what the provider answered.
type Quote struct {
	ResultID string // the source result
	Model    string // the id of the model as listed (topaz:<id>)
	Plan     Plan
	Estimate cloudenhance.CloudEstimate
	Expires  time.Time
}

// Estimates keeps the quotes the user has yet to confirm, in memory, by an opaque id (a restart drops them: the user asks again; nothing was
// charged by a quote). Safe for concurrent use.
type Estimates struct {
	mu     sync.Mutex
	now    func() time.Time
	quotes map[string]Quote
}

// NewEstimates returns an empty store that takes the time from now (nil: time.Now).
func NewEstimates(now func() time.Time) *Estimates {
	if now == nil {
		now = time.Now
	}
	return &Estimates{now: now, quotes: map[string]Quote{}}
}

// Put keeps q for EstimateTTL and returns its id and when it expires. The quotes that are over are dropped.
func (e *Estimates) Put(q Quote) (id string, expires time.Time) {
	var b [16]byte
	_, _ = rand.Read(b[:])
	id = "ve-" + hex.EncodeToString(b[:])
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	for k, v := range e.quotes {
		if !v.Expires.After(now) {
			delete(e.quotes, k)
		}
	}
	q.Expires = now.Add(EstimateTTL)
	e.quotes[id] = q
	return id, q.Expires
}

// Get returns the quote of id; false when there is none or it is over.
func (e *Estimates) Get(id string) (Quote, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	q, ok := e.quotes[id]
	if ok && !q.Expires.After(e.now()) {
		delete(e.quotes, id)
		return Quote{}, false
	}
	return q, ok
}

// Take returns the quote of id and drops it: a quote runs once (a second run of the same would be charged again, and two calls at once
// cannot both have it). False when there is none or it is over.
func (e *Estimates) Take(id string) (Quote, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	q, ok := e.quotes[id]
	delete(e.quotes, id)
	return q, ok && q.Expires.After(e.now())
}
