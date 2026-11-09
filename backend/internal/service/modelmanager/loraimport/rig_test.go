package loraimport_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

type rig struct {
	t      *testing.T
	svc    *loraimport.Service
	layout store.Layout
	state  *store.Store
}

func newRig(t *testing.T, mod func(*loraimport.Deps)) *rig {
	t.Helper()
	layout := store.Layout{Root: filepath.Join(t.TempDir(), "comfyui")}
	st, err := store.Open(layout)
	require.NoError(t, err)
	d := loraimport.Deps{
		Layout: layout, State: st, Now: func() time.Time { return time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC) },
		Free: func(string) uint64 { return 1 << 40 }, Supported: true, MaxBytes: 1 << 20, Lock: func(...string) func() { return func() {} },
	}
	if mod != nil {
		mod(&d)
	}
	return &rig{t: t, svc: loraimport.New(d), layout: layout, state: st}
}

func (r *rig) lorasDir() string { return filepath.Join(r.layout.DataDir(), "models", "loras") }

func (r *rig) stage(name string, body []byte) (loraimport.Staged, error) {
	return r.svc.Stage(context.Background(), name, int64(len(body)), bytes.NewReader(body))
}

func (r *rig) mustStage(name string, body []byte) loraimport.Staged {
	r.t.Helper()
	st, err := r.stage(name, body)
	require.NoError(r.t, err)
	return st
}

func (r *rig) put(name string, body []byte) {
	r.t.Helper()
	require.NoError(r.t, os.MkdirAll(r.lorasDir(), 0o750))
	require.NoError(r.t, os.WriteFile(filepath.Join(r.lorasDir(), name), body, 0o600))
}

func (r *rig) files() []string {
	entries, _ := os.ReadDir(r.lorasDir())
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func (r *rig) leftovers() []string {
	entries, _ := os.ReadDir(r.layout.ImportsDir())
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func refusal(t *testing.T, err error) string {
	t.Helper()
	var inv *modelsettings.InvalidError
	require.ErrorAs(t, err, &inv)
	return inv.Message
}
