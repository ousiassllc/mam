// Package facemodelstest is the face check models over a fake GitHub for the tests (Issue #1026): the two model files the asset set pins, served by a local
// server, installed with the real installer into a temporary model folder, read by a fake face engine that loads like the real one (it needs both files)
// behind the real holder. No ONNX Runtime and no real model is involved.
package facemodelstest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/facesim/holder"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/facemodels"
)

// Sum is the SHA-256 of b as hex, the way the asset set pins a file.
func Sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Content is the content of a model file of the fake GitHub (a few bytes: only the names, sizes and hashes matter to the installer).
func Content(name string) []byte { return []byte("onnx:" + name) }

// Engine is the fake face engine: every image has one face with the same embedding, so the similarity of two pictures is 1. Closed counts the Close calls.
type Engine struct {
	closed *atomic.Int32
	// detector is the model file the engine was loaded from: Close counts, in Rig.ClosedWithFiles, that it was still there (the engine is let go of before the files go).
	detector string
	withFile *atomic.Int32
}

// Faces implements facesim.Engine.
func (e Engine) Faces(_ context.Context, img image.Image) ([]facesim.Face, error) {
	b := img.Bounds()
	return []facesim.Face{{Box: b, Score: 0.99, Embedding: facesim.Embedding{1, 0}}}, nil
}

// Close implements holder.Loaded.
func (e Engine) Close() error {
	if _, err := os.Stat(e.detector); err == nil {
		e.withFile.Add(1)
	}
	e.closed.Add(1)
	return nil
}

// Rig is the face models service over the fake GitHub, with its engine.
type Rig struct {
	Svc *facemodels.Service
	// Engine is the holder the service releases and the generation test reads.
	Engine *holder.Holder
	// Dir is the model folder; Requests counts the requests the fake GitHub answered.
	Dir      string
	Requests *atomic.Int32
	// Loaded and Closed count the engines that were loaded and closed; ClosedWithFiles how many of those were closed while the model files were still in the folder.
	Loaded, Closed, ClosedWithFiles *atomic.Int32
	// Gate, when set, holds every download of the fake GitHub until the channel is closed (an install that is in flight).
	Gate atomic.Pointer[chan struct{}]
	// RuntimeMissing makes loading fail as if the ONNX Runtime library were missing, models or not.
	RuntimeMissing *atomic.Bool
	// Corrupt serves other bytes under the pinned hash (the file of the fake GitHub that changed), once set.
	Corrupt *atomic.Bool
}

var testTime = time.Unix(1_700_000_000, 0)

// New is a Rig with the models not installed yet.
func New(t testing.TB) *Rig {
	t.Helper()
	reqs, corrupt := &atomic.Int32{}, &atomic.Bool{}
	rig := &Rig{Requests: reqs, Loaded: &atomic.Int32{}, Closed: &atomic.Int32{}, ClosedWithFiles: &atomic.Int32{}, RuntimeMissing: &atomic.Bool{}, Corrupt: corrupt}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		if gate := rig.Gate.Load(); gate != nil {
			select {
			case <-*gate:
			case <-r.Context().Done():
				return
			}
		}
		for _, name := range []string{facesim.DetectorFile, facesim.RecognizerFile} {
			if r.URL.Path == "/models/"+name {
				body := Content(name)
				if corrupt.Load() {
					body = []byte("changed upstream:" + name) // not the pinned bytes, and not the pinned size
				}
				http.ServeContent(w, r, name, testTime, bytes.NewReader(body))
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	dir := filepath.Join(t.TempDir(), "models")
	rig.Dir = dir
	rig.Engine = holder.New(func() (holder.Loaded, error) {
		for _, name := range []string{facesim.DetectorFile, facesim.RecognizerFile} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				return nil, fmt.Errorf("%w: %w", facesim.ErrUnavailable, err)
			}
		}
		if rig.RuntimeMissing.Load() {
			return nil, fmt.Errorf("%w: %w", facesim.ErrUnavailable, errors.New("onnxruntime.dll: module not found"))
		}
		rig.Loaded.Add(1)
		return Engine{closed: rig.Closed, detector: filepath.Join(dir, facesim.DetectorFile), withFile: rig.ClosedWithFiles}, nil
	}, facesim.ONNXConfig{ModelDir: dir}.Diagnose)

	source := catalog.ResourceSource{Name: "opencv_zoo", URL: "https://github.com/opencv/opencv_zoo"}
	set := catalog.ResourceSet{
		Runtime: facemodels.SetID,
		Licenses: []catalog.ResourceLicense{
			{ID: "mit", Name: "MIT", URL: "https://example.com/mit", Summary: "s", Commercial: "yes"},
			{ID: "apache", Name: "Apache-2.0", URL: "https://example.com/apache", Summary: "s", Commercial: "yes", Notice: "学習データの権利は確かめられていません"},
		},
	}
	for name, license := range map[string]string{facesim.DetectorFile: "mit", facesim.RecognizerFile: "apache"} {
		data := Content(name)
		set.Assets = append(set.Assets, catalog.Resource{
			Name: name, Description: "d", Dest: name, URL: srv.URL + "/models/" + name, SHA256: Sum(data), Size: int64(len(data)), Required: true, License: license, Source: source,
		})
	}
	svc, err := facemodels.New(facemodels.Deps{
		StateDir: filepath.Join(t.TempDir(), "state"), ModelDir: dir, Catalog: &catalog.Catalog{ResourceSets: []catalog.ResourceSet{set}}, Engine: rig.Engine,
		Fetch: fetch.Options{
			CheckURL: func(*url.URL) error { return nil },
			Client:   fetch.WithRedirectRules(&http.Client{}, func(*url.URL) error { return nil }, ""),
			Retry:    fetch.Retry{Attempts: 1},
		},
		FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
	})
	require.NoError(t, err)
	rig.Svc = svc
	t.Cleanup(func() { _ = rig.Engine.Close() })
	return rig
}

// Install installs the models the way the user does.
func (r *Rig) Install(t testing.TB) {
	t.Helper()
	require.NoError(t, r.Svc.Install(context.Background(), true, nil))
}
