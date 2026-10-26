package generation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

var (
	fakePNG = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	fakeMP4 = append([]byte("\x00\x00\x00\x18ftypisom"), bytes.Repeat([]byte{0}, 32)...)
)

const (
	plannerSpec = `{"shot_objective":"Character notices something behind her and starts running","duration":"4.0s",
		"character":{"id":"Character_A","identity":"locked","wardrobe":"inherit_previous","hair":"inherit_previous"},
		"acting":[{"time":"0.0-2.0","action":"turn head right"},{"time":"2.0-4.0","action":"begin running"}],
		"camera":{"lens":"35mm","framing":"medium","movement":"slow_push_in"},"lighting":{"mood":"cold blue"},
		"continuity":{"start_from_previous_final_state":true},"constraints":["preserve_face_identity"]}`
	directorPlan = `{"references":[
		{"kind":"character","name":"Mio","prompt":"A young woman in a black coat, full body, neutral background"},
		{"kind":"product","name":"Serum","prompt":"A glass serum bottle, front view, sharp label","use_uploaded":false},
		{"kind":"location","name":"Street","prompt":"An empty rainy street at dusk, wide view"},
		{"kind":"style","prompt":"Cold blue palette with fine film grain"},
		{"kind":"lighting","prompt":"Soft key light from the left on a simple subject"},
		{"kind":"keyframe","prompt":"The woman in the black coat on the rainy street, medium shot"}]}`
	extractorDiff = `{"changes":[{"target":"camera","field":"movement","value":"static"}]}`
)

// reasoner answers the Reasoning LLM agents with canned JSON and counts the calls per agent.
type reasoner struct {
	mu    sync.Mutex
	users map[llm.Agent][]string
	// errs makes an agent fail.
	errs map[llm.Agent]error
	// texts replaces the canned answer of an agent.
	texts map[llm.Agent]string
}

func (r *reasoner) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.users == nil {
		r.users = map[llm.Agent][]string{}
	}
	r.users[agent] = append(r.users[agent], req.User)
	if err := r.errs[agent]; err != nil {
		return llm.Response{}, err
	}
	text := map[llm.Agent]string{
		llm.AgentPromptPlanner:  plannerSpec,
		llm.AgentArtDirector:    directorPlan,
		llm.AgentStateExtractor: extractorDiff,
	}[agent]
	if t, ok := r.texts[agent]; ok {
		text = t
	}
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

func (r *reasoner) calls(agent llm.Agent) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.users[agent])
}

func (r *reasoner) lastUser(agent llm.Agent) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u := r.users[agent]; len(u) > 0 {
		return u[len(u)-1]
	}
	return ""
}

// videoFake is the video backend of the models. The n-th call (1-based, in arrival order) fails
// when failOn(n) says so. block, when set, holds every call until it is closed or the call is cancelled.
type videoFake struct {
	mu       sync.Mutex
	calls    int
	requests []modeladapter.VideoRequest
	failOn   func(n int) error
	block    chan struct{}
	entered  chan struct{}
}

func (v *videoFake) GenerateVideo(ctx context.Context, _ string, req modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	v.mu.Lock()
	v.calls++
	n := v.calls
	v.requests = append(v.requests, req)
	failOn, block, entered := v.failOn, v.block, v.entered
	v.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return modeladapter.VideoResult{}, ctx.Err()
		}
	}
	if failOn != nil {
		if err := failOn(n); err != nil {
			return modeladapter.VideoResult{}, err
		}
	}
	return modeladapter.VideoResult{File: modeladapter.BlobOf(fakeMP4), MimeType: "video/mp4"}, nil
}

func (v *videoFake) callCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.calls
}

func (v *videoFake) sent() []modeladapter.VideoRequest {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]modeladapter.VideoRequest(nil), v.requests...)
}

// imageFake is the image backend of the models.
type imageFake struct {
	mu    sync.Mutex
	calls int
	// failOn fails the n-th call (1-based, in arrival order) when it returns an error.
	failOn func(n int) error
}

func (i *imageFake) GenerateImage(context.Context, string, modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.calls++
	if i.failOn != nil {
		if err := i.failOn(i.calls); err != nil {
			return modeladapter.ImageResult{}, err
		}
	}
	return modeladapter.ImageResult{Data: fakePNG, MimeType: "image/png"}, nil
}

func (i *imageFake) callCount() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.calls
}

// judgeFake is the AI Video Judge. Evaluate answers with verdicts(n) for its n-th call (1-based).
type judgeFake struct {
	mu       sync.Mutex
	started  []string
	startErr error
	verdicts func(n int) (domain.JudgeResult, error)
	judged   map[string]domain.JudgeResult
	evals    int
}

func (j *judgeFake) Start(_ context.Context, takeID string) (domain.Job, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.startErr != nil {
		return domain.Job{}, j.startErr
	}
	j.started = append(j.started, takeID)
	return domain.Job{ID: "judge-" + takeID, Kind: domain.JobJudge}, nil
}

func (j *judgeFake) Evaluate(_ context.Context, takeID string) (domain.JudgeResult, error) {
	j.mu.Lock()
	j.evals++
	n := j.evals
	j.mu.Unlock()
	res := domain.JudgeResult{Result: domain.JudgePass, OverallConfidence: 0.9}
	if j.verdicts != nil {
		var err error
		if res, err = j.verdicts(n); err != nil {
			return domain.JudgeResult{}, err
		}
	}
	res.TakeID = takeID
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.judged == nil {
		j.judged = map[string]domain.JudgeResult{}
	}
	j.judged[takeID] = res
	return res, nil
}

func (j *judgeFake) startedIDs() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.started...)
}

func (j *judgeFake) results() map[string]domain.JudgeResult {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := map[string]domain.JudgeResult{}
	for k, v := range j.judged {
		out[k] = v
	}
	return out
}

// chatFake records what the Orchestrator announces in the thread.
type chatFake struct {
	mu   sync.Mutex
	msgs []domain.ChatMessage
}

func (c *chatFake) Announce(_ context.Context, _ string, m domain.ChatMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, m)
	return nil
}

func (c *chatFake) ofKind(k domain.MessageKind) []domain.ChatMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []domain.ChatMessage
	for _, m := range c.msgs {
		if m.Kind == k {
			out = append(out, m)
		}
	}
	return out
}

// jobsFake is the job queue: it only records the enqueued jobs; the test runs them with Handle.
type jobsFake struct {
	mu   sync.Mutex
	n    int
	err  error
	jobs []domain.Job
}

func (q *jobsFake) Enqueue(_ context.Context, kind domain.JobKind, payload any) (domain.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return domain.Job{}, q.err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return domain.Job{}, err
	}
	q.n++
	job := domain.Job{ID: fmt.Sprintf("job-%d", q.n), Kind: kind, Payload: raw, Status: domain.JobQueued}
	q.jobs = append(q.jobs, job)
	return job, nil
}

func (q *jobsFake) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.jobs)
}

// progressLog collects the Notify calls.
type progressLog struct {
	mu   sync.Mutex
	list []generation.Progress
}

func (p *progressLog) add(pr generation.Progress) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.list = append(p.list, pr)
}

func (p *progressLog) all() []generation.Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]generation.Progress(nil), p.list...)
}
