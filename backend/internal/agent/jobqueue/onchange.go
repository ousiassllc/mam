package jobqueue

import "github.com/ousiassllc/mam/backend/internal/bgwork"

// OnChange installs a callback invoked after every status change of a job (queued,
// running, succeeded, failed, requeued) with the job ID; the chat uses it to refresh
// progress cards. fn runs on the caller's goroutine and MUST NOT block; a panic in it is
// logged with its stack and the worker goes on. It MUST be called before Start.
func (q *Queue) OnChange(fn func(jobID string)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.started {
		panic("jobqueue: OnChange called after Start")
	}
	q.onChange = fn
}

func (q *Queue) changed(jobID string) {
	q.mu.Lock()
	fn := q.onChange
	q.mu.Unlock()
	if fn != nil {
		// The callback runs on the worker goroutine, outside invoke's recover: a panic in it must not end the server.
		bgwork.RunSafely(q.logger, "job change callback", func() { fn(jobID) })
	}
}
