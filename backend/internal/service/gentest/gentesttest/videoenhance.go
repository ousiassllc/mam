package gentesttest

import (
	"context"
	"os"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/enhancevideo"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
)

// The video enhancement workflows WithVideoEnhanceModels adds, each the bundled template as it is shipped: UpscaleVideoModel is SeedVR2 3B,
// InterpolateVideoModel is RIFE 4.25.
var (
	UpscaleVideoModel     = templateModel("video-upscale-seedvr2-3b")
	InterpolateVideoModel = templateModel("video-interpolate-rife-v4.25")
)

var videoEnhanceTemplateOf = map[domain.ModelName]string{UpscaleVideoModel: "video-upscale-seedvr2-3b", InterpolateVideoModel: "video-interpolate-rife-v4.25"}

// enhanceBackend answers "make the video larger / smoother" with a fixed file.
type enhanceBackend struct {
	f *Fake
	m domain.ModelName
}

func (b enhanceBackend) GenerateVideo(context.Context, string, modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	return modeladapter.VideoResult{}, enhancevideo.ErrNotEnhance
}

func (b enhanceBackend) EnhanceVideo(ctx context.Context, req enhancevideo.Request) (modeladapter.VideoResult, error) {
	if err := b.f.enter(ctx, b.m, Call{Model: string(b.m), Owner: live.OwnerOf(ctx), Enhance: &req}); err != nil {
		return modeladapter.VideoResult{}, err
	}
	return modeladapter.VideoResult{File: modeladapter.BlobOf([]byte("ENHANCED")), MimeType: "video/mp4"}, nil
}

// WithVideoEnhanceModels adds UpscaleVideoModel and InterpolateVideoModel to the rig, with a fake backend, at the endpoint "bundled-comfyui" (a
// closed port: nothing is run). The other models of the rig stay.
func WithVideoEnhanceModels() Option {
	return func(r *Rig, d *gentest.Deps) {
		if r.Config.Endpoints == nil {
			r.Config.Endpoints = map[string]localcfg.Endpoint{}
		}
		if r.Config.LocalModels == nil {
			r.Config.LocalModels = map[string]localcfg.Model{}
		}
		r.Config.Endpoints[EndpointName] = localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}
		for m, id := range videoEnhanceTemplateOf {
			tpl, ok := localcfg.TemplateByID(id)
			if !ok {
				panic("gentesttest: the template " + id + " is gone")
			}
			_, _, name, _ := localcfg.ParseID(string(m))
			r.Config.LocalModels[localcfg.Key(EndpointName, name)] = tpl.Model
			r.enhance = append(r.enhance, m)
		}
		d.BundledEndpoint = EndpointName
		localbackends.Publish(r.Config)
		r.T.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	}
}

// FakeProbe is the ffprobe of the rig: it answers Info for every file (Err instead when set).
type FakeProbe struct {
	Info media.Info
	Err  error
}

// Probe implements venhance.Prober.
func (p FakeProbe) Probe(context.Context, string) (media.Info, error) { return p.Info, p.Err }

// A480p16 is a clip of the usual size of a Wan 2.2 result: 480 × 848, 16 fps, 81 frames, with sound.
var A480p16 = media.Info{Duration: 5.0625, VideoDuration: 5.0625, FPS: 16, FrameRate: "16", Width: 480, Height: 848, FrameCount: 81, HasAudio: true}

// A1080p24 is a full HD clip of 5 seconds at 24 fps.
var A1080p24 = media.Info{Duration: 5, VideoDuration: 5, FPS: 24, FrameRate: "24", Width: 1920, Height: 1080, FrameCount: 120}

// WithProbe gives the service an ffprobe that answers info for every video (the default of the rig has none: no ffmpeg).
func WithProbe(p venhance.Prober) Option {
	return func(_ *Rig, d *gentest.Deps) { d.Probe = p }
}

// WithCloud gives the service a paid cloud of video enhancement.
func WithCloud(c cloudenhance.Cloud) Option {
	return func(_ *Rig, d *gentest.Deps) { d.Cloud = c }
}

// FakeCloud is a cloudenhance.Cloud of the rig: Estimate answers Quote (or Err), Run writes Result to the destination and records what it was given.
type FakeCloud struct {
	IsConfigured bool
	ModelList    []cloudenhance.CloudModel
	Quote        cloudenhance.CloudEstimate
	EstimateErr  error
	RunErr       error
	Result       []byte
	// HoldRun makes Run signal RunStarted (a channel the test makes) and wait until its context ends, then fail as a job that Topaz accepted does
	// when it is stopped: with modeladapter.AfterSubmit.
	HoldRun    bool
	RunStarted chan struct{}

	mu        sync.Mutex
	Estimates []cloudenhance.CloudRequest
	Runs      []cloudenhance.RunInput
	Sources   [][]byte // the content of the source file at the time of Run
}

// Configured implements cloudenhance.Cloud.
func (c *FakeCloud) Configured() bool { return c.IsConfigured }

// Models implements cloudenhance.Cloud.
func (c *FakeCloud) Models() []cloudenhance.CloudModel { return c.ModelList }

// Estimate implements cloudenhance.Cloud.
func (c *FakeCloud) Estimate(_ context.Context, r cloudenhance.CloudRequest) (cloudenhance.CloudEstimate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Estimates = append(c.Estimates, r)
	return c.Quote, c.EstimateErr
}

// Run implements cloudenhance.Cloud.
func (c *FakeCloud) Run(ctx context.Context, in cloudenhance.RunInput, progress func(cloudenhance.Progress)) (int64, error) {
	c.mu.Lock()
	src, _ := os.ReadFile(in.SourcePath)
	c.Runs, c.Sources = append(c.Runs, in), append(c.Sources, src)
	c.mu.Unlock()
	if progress != nil {
		progress(cloudenhance.Progress{Status: "processing", Percent: 50})
	}
	if c.HoldRun {
		c.RunStarted <- struct{}{}
		<-ctx.Done()
		return 0, modeladapter.AfterSubmit(ctx.Err())
	}
	if c.RunErr != nil {
		return 0, c.RunErr
	}
	return int64(len(c.Result)), os.WriteFile(in.DestPath, c.Result, 0o600)
}
