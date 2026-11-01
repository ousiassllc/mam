package mmtest

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// pythonExe is the interpreter path of the upstream package; the directory name is spelled that way upstream.
const pythonExe = "python_embeded/python.exe" //nolint:misspell // upstream spelling

// File is a model file the fake Hugging Face serves.
type File struct {
	Folder, Name string
	Body         []byte
}

func (f File) sha() string { return sumOf(f.Body) }

func sumOf(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// The files of the test catalog: m-a is one file, m-b and m-c two each, sharing the VAE.
var (
	CkptA = File{"checkpoints", "a.safetensors", []byte(strings.Repeat("A", 200_000))}
	UnetB = File{"diffusion_models", "b.safetensors", []byte(strings.Repeat("B", 300_000))}
	VaeBC = File{"vae", "shared.safetensors", []byte(strings.Repeat("V", 100_000))}
	ClipC = File{"text_encoders", "c.safetensors", []byte(strings.Repeat("C", 150_000))}
	// LoraD is the LoRA of the adult model m-d (a third party's mirror), which also holds CkptA as its base.
	LoraD   = File{"loras", "d.safetensors", []byte(strings.Repeat("D", 50_000))}
	AllFile = []File{CkptA, UnetB, VaeBC, ClipC, LoraD}
)

// AdultNoticeSource is the SourceNote of the mirrored LoRA of m-d.
const AdultNoticeSource = "第三者のミラー。原本は Civitai（model 1）で、SHA-256 が一致することを 2026-10-25 に確かめた。"

// Host is the fake GitHub / Hugging Face: it serves the engine archive and the model files.
type Host struct {
	*httptest.Server
	mu      sync.Mutex
	files   map[string]File
	archive []byte
	n       int
	holds   map[string]*Hold
	drops   map[string]*drop
}

// drop cuts the connection of the next times requests for a path after bytes of the body went out.
type drop struct{ bytes, times int }

// Drop makes the host cut the connection of the next times requests for the file after bytes of the body were sent (a
// Hugging Face CDN that resets the connection of a long download): the client keeps a part file and has to resume it.
func (h *Host) Drop(f File, bytes, times int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.drops["/"+f.Folder+"/"+f.Name] = &drop{bytes: bytes, times: times}
}

// cutWriter ends the connection (the handler aborts) once limit bytes of the body were written.
type cutWriter struct {
	http.ResponseWriter
	limit int
}

func (c *cutWriter) Write(p []byte) (int, error) {
	if len(p) < c.limit {
		c.limit -= len(p)
		return c.ResponseWriter.Write(p)
	}
	_, _ = c.ResponseWriter.Write(p[:c.limit])
	c.ResponseWriter.(http.Flusher).Flush()
	panic(http.ErrAbortHandler) // closes the connection with the body unfinished
}

// Gone makes the host answer 404 for the file from now on: the mirror of a third party was deleted.
func (h *Host) Gone(f File) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.files, "/"+f.Folder+"/"+f.Name)
}

// Requests is how many requests reached the host.
func (h *Host) Requests() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n
}

// archivePath is the synthetic 7z of the extract tests (LZMA2 + BCJ2 like the upstream package), found from the
// module root above the test's working directory (runtime.Caller is a module path under -trimpath).
func archivePath() string {
	dir, _ := os.Getwd()
	for ; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
	}
	return filepath.Join(dir, "backend", "internal", "modelmgr", "extract", "testdata", "bcj2.7z")
}

func newHost(t *testing.T) *Host {
	t.Helper()
	h := &Host{files: map[string]File{}, holds: map[string]*Hold{}, drops: map[string]*drop{}}
	var err error
	h.archive, err = os.ReadFile(archivePath())
	require.NoError(t, err)
	for _, f := range AllFile {
		h.files["/"+f.Folder+"/"+f.Name] = f
	}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.n++
		f, ok := h.files[r.URL.Path]
		hold := h.holds[r.URL.Path]
		out := w
		if d := h.drops[r.URL.Path]; d != nil && d.times > 0 {
			d.times--
			out = &cutWriter{ResponseWriter: w, limit: d.bytes}
		}
		h.mu.Unlock()
		if hold != nil {
			hold.wait(r)
		}
		body := f.Body
		if r.URL.Path == "/engine.7z" {
			ok, body = true, h.archive
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(out, r, "f", time.Time{}, strings.NewReader(string(body)))
	}))
	t.Cleanup(h.Close)
	return h
}

func testCatalog(h *Host) *catalog.Catalog {
	lic := func(id string, commercial bool) catalog.License {
		return catalog.License{ID: id, Version: "1", Name: id, URL: "https://example.com/" + id, Summary: "summary of " + id, CommercialUse: commercial, ExcludedRegions: regionsOf(id)}
	}
	cf := func(f File) catalog.File {
		return catalog.File{Name: f.Name, Folder: f.Folder, URL: h.URL + "/" + f.Folder + "/" + f.Name, SHA256: f.sha(), Size: int64(len(f.Body))}
	}
	mirror := cf(LoraD)
	mirror.SourceNote = AdultNoticeSource
	vram := catalog.VRAM{MinGB: 8, Source: "test", Estimate: true}
	return &catalog.Catalog{
		Engine: catalog.Engine{
			Version: "v1.0.0", SourceURL: "https://github.com/Comfy-Org/ComfyUI/tree/v1.0.0", ArchiveRoot: "ComfyUI_windows_portable",
			Python: pythonExe, Main: "ComfyUI/main.py", License: catalog.License{ID: "gpl-3.0", Version: "3", Name: "GPL-3.0", URL: "https://example.com/gpl"},
			Variants: map[catalog.Variant]catalog.Asset{
				catalog.VariantNVIDIA: {Label: "NVIDIA", URL: h.URL + "/engine.7z", SHA256: sumOf(h.archive), Size: int64(len(h.archive)), ExtractedSize: 1 << 20},
			},
		},
		Models: []catalog.Model{
			{
				ID: "m-a", Name: "A", Kind: localcfg.MediaImage, Tier: catalog.TierStandard, Description: "d", Files: []catalog.File{cf(CkptA)}, VRAM: vram,
				License: lic("apache-2.0", true), Workflows: []catalog.Workflow{{Name: "wf-a", Template: "txt2img-sdxl"}, {Name: "wf-a-i2i", Template: "img2img-sdxl"}},
			},
			{
				ID: "m-b", Name: "B", Kind: localcfg.MediaVideo, Tier: catalog.TierStandard, Description: "d", Files: []catalog.File{cf(UnetB), cf(VaeBC)}, VRAM: vram,
				License: lic("apache-2.0", true), Workflows: []catalog.Workflow{{Name: "wf-b", Template: "txt2video-wan22-5b"}},
			},
			{
				ID: "m-c", Name: "C", Kind: localcfg.MediaImage, Tier: catalog.TierAdvanced, Description: "d", Files: []catalog.File{cf(ClipC), cf(VaeBC)}, VRAM: vram,
				License: lic("non-commercial", false), ConsentRequired: true, Workflows: []catalog.Workflow{{Name: "wf-c", Template: "txt2img-flux-schnell"}},
			},
			{
				// an adult LoRA model: hidden until the age is confirmed; its base file is shared with m-a
				ID: "m-d", Name: "D", Kind: localcfg.MediaImage, Tier: catalog.TierAdvanced, ContentRating: catalog.RatingAdult, Description: "d",
				TriggerWords: []string{"nsfwsks"}, Files: []catalog.File{cf(CkptA), mirror}, VRAM: vram,
				License: lic("unspecified", false), ConsentRequired: true, Workflows: []catalog.Workflow{{Name: "wf-d", Template: "txt2img-flux-dev-lora"}},
			},
		},
	}
}

func regionsOf(id string) []string {
	if id == "non-commercial" {
		return []string{"EU", "KR"}
	}
	return nil
}

// Hold stops the answer to a request for a path until it is released or the client gave up (a cancelled download):
// a test can act while a download is in flight.
type Hold struct {
	reached chan struct{}
	release chan struct{}
	once    sync.Once
}

// Hold makes the host keep every request for path waiting.
func (h *Host) Hold(path string) *Hold {
	hold := &Hold{reached: make(chan struct{}, 16), release: make(chan struct{})}
	h.mu.Lock()
	h.holds[path] = hold
	h.mu.Unlock()
	return hold
}

func (o *Hold) wait(r *http.Request) {
	o.reached <- struct{}{}
	select {
	case <-o.release:
	case <-r.Context().Done():
	}
}

// Reached waits until a request for the path is being held.
func (o *Hold) Reached(t *testing.T) {
	t.Helper()
	select {
	case <-o.reached:
	case <-time.After(20 * time.Second):
		t.Fatal("the request never reached the host")
	}
}

// Release lets the held requests (and later ones) through.
func (o *Hold) Release() { o.once.Do(func() { close(o.release) }) }
