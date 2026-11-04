// Package facemodels is the face check's models in the model manager (Issue #1026): the YuNet and SFace ONNX files that the face similarity of the
// generation test (and the Judge's Face Identity Similarity) reads from the model folder (config.FaceModels). They are not part of mam: the user installs
// them with one button, and the service downloads the pinned files of the catalog's asset set "onnx-face-models" (a commit of opencv/opencv_zoo, SHA-256 and
// size checked) with the same installer the local runtimes' weights and the pose library use (modelmgr/runtime), straight into the model folder.
//
// The engine that reads them (facesim/holder) is loaded the first time it is used, so an install turns the face similarity on without a restart,
// and it is let go of before the files are deleted (Windows keeps a loaded model's file open).
package facemodels

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/facesim/holder"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// SetID is the id of the asset set in the catalog (data/assets/onnx-face-models.json).
const SetID = "onnx-face-models"

// Deps are what the Service needs.
type Deps struct {
	// StateDir keeps the install's record (which files were verified); it is not the model folder.
	StateDir string
	// ModelDir is the folder the face models are installed to and read from (config.FaceModels).
	ModelDir string
	// Catalog is the catalog (nil: catalog.Default()).
	Catalog *catalog.Catalog
	// Fetch are the options of the download (client, URL check); the progress is set per call.
	Fetch fetch.Options
	// FreeBytes reports the free disk space (nil: the real one).
	FreeBytes func(path string) (uint64, error)
	// Engine is the face engine that reads the files: it is let go of before they are replaced or deleted, and asked why it is off. Nil: none.
	Engine *holder.Holder
}

// Service is the installed (or installable) set of face models.
type Service struct {
	set      catalog.ResourceSet
	modelDir string
	install  *rtm.Installer
	engine   *holder.Holder
	mu       sync.Mutex // one install or removal at a time
	prog     progress   // read by Progress without mu: an install holds mu until it is done
}

// New opens the service. It reads the record of the install and fails when the catalog has no set onnx-face-models.
func New(d Deps) (*Service, error) {
	if d.Catalog == nil {
		d.Catalog = catalog.Default()
	}
	set, ok := d.Catalog.ResourceSet(SetID)
	if !ok {
		return nil, fmt.Errorf("facemodels: the catalog has no asset set %q", SetID)
	}
	layout := store.RuntimeLayout{Root: d.StateDir, Env: d.ModelDir}
	st, err := store.OpenRuntime(layout)
	if err != nil {
		return nil, fmt.Errorf("facemodels: %w", err)
	}
	return &Service{
		set: set, modelDir: d.ModelDir, engine: d.Engine,
		install: &rtm.Installer{Layout: layout, AssetSet: set, Store: st, Fetch: d.Fetch, FreeBytes: d.FreeBytes},
	}, nil
}

// ErrNotAccepted is the refusal of an install the user did not agree to.
var ErrNotAccepted = gterr.Invalid("顔チェック用モデルのライセンス（YuNet は MIT・SFace は Apache-2.0）と取得元を確認して、同意してから導入してください")

// Install downloads what is missing or is not the pinned file into the model folder, checks every file against its SHA-256 and size, and leaves the engine to
// load them on its next use (resumable: the next call continues from the part that was downloaded). It refuses at once, without fetching, when accepted is
// false. progress gets the bytes done and the bytes in all (it must not block; nil for none). A failure is a *gterr.InvalidError that says what happened.
func (s *Service) Install(ctx context.Context, accepted bool, progress func(done, total int64)) error {
	if !accepted {
		return ErrNotAccepted
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.install.NotReady()) > 0 && s.engine != nil {
		_ = s.engine.Release(nil) // a file that is not the pinned one is replaced below: the engine must not hold it (Windows)
	}
	s.prog.start()
	defer s.prog.stop()
	err := s.install.EnsureAssets(ctx, func(_ rtm.Phase, _ string, done, total int64) {
		s.prog.set(done, total)
		if progress != nil {
			progress(done, total)
		}
	})
	if err != nil {
		return userError(ctx, err)
	}
	return nil
}

// Remove deletes the two model files (and unfinished downloads) and the record of the install, after the engine let go of them; other files in the folder
// stay. Models that are not installed are removed without an error. A running install is a conflict. The face similarity is off from then on.
func (s *Service) Remove() error {
	if s.prog.running.Load() {
		return gterr.Conflict("顔チェック用モデルを導入している最中は削除できません。終わってからやり直してください")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	remove := s.install.RemoveAssets
	var err error
	if s.engine != nil {
		err = s.engine.Release(remove)
	} else {
		err = remove()
	}
	if err != nil {
		return gterr.Invalid("顔チェック用モデルのファイルを削除できませんでした（使用中の可能性があります。mam を再起動してからやり直してください）: %v", err)
	}
	return nil
}

// userError is the message of a failed install (a cancelled one is left as it is: the caller decides what an interruption means).
func userError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return err
	}
	var ns *fetch.NoSpaceError
	var he *fetch.HTTPError
	switch {
	case errors.As(err, &ns):
		return gterr.Invalid("空き容量が足りません（必要 %s、空き %s、場所 %s）", fetch.HumanBytes(ns.Need), fetch.HumanBytes(ns.Free), ns.Path)
	case errors.Is(err, fetch.ErrChecksum):
		return gterr.Invalid("ダウンロードしたファイルの SHA-256 が、固定した値と一致しません。ダウンロードを破棄しました（取得元の更新か転送の破損です。MAM の更新を待つか、もう一度試してください）")
	case errors.Is(err, fetch.ErrSize):
		return gterr.Invalid("ダウンロードしたファイルの大きさが固定した値と違います。ダウンロードを破棄しました")
	case errors.As(err, &he) && he.Definitive():
		return gterr.Invalid("取得元が応答しません（HTTP %d）。顔モデルのリポジトリ（opencv/opencv_zoo）が移動または削除された可能性があります", he.Status)
	case errors.Is(err, store.ErrNotWritable):
		return gterr.Invalid("顔モデルの記録の置き場所に書き込めません: %v", err)
	}
	return gterr.Invalid("顔チェック用モデルを取得できませんでした: %v", err)
}

// ModelDir is the folder the models are installed to.
func (s *Service) ModelDir() string { return filepath.Clean(s.modelDir) }
