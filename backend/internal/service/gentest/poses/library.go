// Package poses is the pose library of the generation test: the mannequin pose pictures of Shisei (https://github.com/wquguru/shisei,
// images CC0 1.0), which the user picks a pose from and gives to an editing model as a reference with the role "pose" (Issue #986).
// The pictures are not part of mam: the first time, the user installs them, and the library downloads the pinned files of the catalog's
// asset set "shisei-poses" (the zips of the release, the files named by their SHA-256, and the pose catalog) with the same installer the
// local runtimes' weights use (modelmgr/runtime), below <exe dir>/localai/shisei-poses. The suggestive series (sexy, duo) are a second asset set,
// "shisei-poses-adult", that is installed and listed only while the age gate of the model manager is open (adult.go; Issue #1020).
package poses

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// SetID is the id of the asset set of the library in the catalog (data/assets/shisei-poses.json) and the name of its directory below localai.
const SetID = "shisei-poses"

const (
	catalogFile = "catalog.json"
	picturesDir = "poses"
	imageExt    = ".webp"
)

// ErrNotInstalled means the pictures of the library are not (all) on the disk, or not verified: the user installs the library first.
var ErrNotInstalled = errors.New("poses: the pose library is not installed")

// Deps are what the Library needs.
type Deps struct {
	// Root is the localai directory next to mam.exe (store.LocalAIRoot).
	Root string
	// Catalog is the catalog (nil: catalog.Default()).
	Catalog *catalog.Catalog
	// Fetch are the options of the download (client, URL check); the progress is set per call.
	Fetch fetch.Options
	// FreeBytes reports the free disk space (nil: the real one).
	FreeBytes func(path string) (uint64, error)
	// AdultOpen says whether the age gate is open (modelmanager.Service.AdultContentEnabled): the user confirmed their age and turned "show adult
	// models" on. The suggestive series (sexy, duo) are installed and listed only while it is. nil: the gate is closed for good.
	AdultOpen func() bool
}

// Library is the installed (or installable) pose library.
type Library struct {
	set     catalog.ResourceSet
	layout  store.RuntimeLayout
	install *rtm.Installer
	adult   *adultSet   // the suggestive series' own asset set (nil: the catalog has none)
	open    func() bool // the age gate (Deps.AdultOpen)
	mu      sync.Mutex  // one install at a time; also guards the cached index
	index   []Pose      // every installed pose, the suggestive ones included: the gate is applied when it is read (Poses)
	general []Pose      // index without the suggestive ones
	prog    progress    // read by Progress without mu: the install holds mu until it is done
}

// New opens the library below d.Root. It reads the state file of the asset set, and fails when the catalog has no set shisei-poses.
func New(d Deps) (*Library, error) {
	if d.Catalog == nil {
		d.Catalog = catalog.Default()
	}
	set, ok := d.Catalog.ResourceSet(SetID)
	if !ok {
		return nil, fmt.Errorf("poses: the catalog has no asset set %q", SetID)
	}
	layout := store.ForRuntime(d.Root, SetID)
	st, err := store.OpenRuntime(layout)
	if err != nil {
		return nil, fmt.Errorf("poses: %w", err)
	}
	adult, err := newAdultSet(d)
	if err != nil {
		return nil, err
	}
	return &Library{
		set: set, layout: layout, adult: adult, open: d.AdultOpen,
		install: &rtm.Installer{Layout: layout, AssetSet: set, Store: st, Fetch: d.Fetch, FreeBytes: d.FreeBytes},
	}, nil
}

// dir is where the files of the library are (the "environment" of the asset set).
func (l *Library) dir() string { return l.layout.EnvDir() }

// Info is what the user is told before the library is installed: where the files come from, how much is downloaded and the license of each.
type Info struct {
	// DownloadBytes is the size of everything the install downloads; NeedBytes the free space an install needs (what is not downloaded yet,
	// and the unpacked files). RemainBytes is how much of the download is still to fetch: less than DownloadBytes when some of the files
	// are in place already (an install that was interrupted, or a library from before the depth and normal maps: Issue #1020).
	DownloadBytes int64
	NeedBytes     int64
	RemainBytes   int64
	// Poses and Images are how many poses and pictures (line art, depth maps and normal maps together) the install puts in place (the pose
	// catalog's own list is only known once installed).
	Poses  int
	Images int
	// Source is the repository the files are from.
	Source string
	// Licenses are the licenses of the asset set (the images, and the catalog's names), each with its commercial-use answer.
	Licenses []catalog.ResourceLicense
}

// Status is Info with whether the library is installed.
type Status struct {
	Info
	Installed bool
	// Adult is the offer of the suggestive series (sexy, duo). It is nil while the age gate is closed (nothing of it is said then) and when the
	// catalog has no such set.
	Adult *AdultStatus
}

// infoOf is what the install of the set would do, against the disk the installer looks at.
func infoOf(set catalog.ResourceSet, in *rtm.Installer) Info {
	src := ""
	if len(set.Assets) > 0 {
		src = set.Assets[0].Source.URL
	}
	poses := map[string]bool{}
	images := 0
	for _, a := range set.Assets {
		for _, m := range a.Members {
			if id, ok := strings.CutSuffix(path.Base(m.Dest), imageExt); ok && imageID.MatchString(id) {
				images++                                      // a pose's depth map has the file name of its line picture, in another directory
				poses[id[:strings.LastIndex(id, "_")]] = true // <series>_<pose id>
			}
		}
	}
	remain := int64(0)
	for i, st := range in.Assets() {
		if st.State != rtm.AssetOK {
			remain += set.Assets[i].Size
		}
	}
	return Info{
		DownloadBytes: set.TotalSize(), NeedBytes: in.AssetNeedBytes(), RemainBytes: remain, Poses: len(poses), Images: images, Source: src, Licenses: set.Licenses,
	}
}

// Status says whether every file of the library is on the disk and verified, and what an install would download.
func (l *Library) Status() Status {
	return Status{Installed: len(l.install.NotReady()) == 0, Info: infoOf(l.set, l.install), Adult: l.adultStatus()}
}

// ErrNotAccepted is the refusal of an install the user did not agree to.
var ErrNotAccepted = gterr.Invalid("ポーズ集のライセンス（画像は CC0 1.0）と取得元を確認して、同意してから導入してください")

// Install downloads what is missing or is not the pinned file, checks every file against its SHA-256 and unpacks the pictures (resumable: the
// next call continues from the part that was downloaded). It refuses at once, without fetching, when accepted is false. progress gets the
// bytes done and the bytes in all (it must not block; nil for none). A failure is a *gterr.InvalidError that says what happened.
func (l *Library) Install(ctx context.Context, accepted bool, progress func(done, total int64)) error {
	if !accepted {
		return ErrNotAccepted
	}
	return l.ensure(ctx, l.install, progress)
}

// ensure installs what the installer's set still lacks, one install at a time, reporting the progress.
func (l *Library) ensure(ctx context.Context, in *rtm.Installer, progress func(done, total int64)) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.index, l.general = nil, nil
	l.prog.start()
	defer l.prog.stop()
	err := in.EnsureAssets(ctx, func(_ rtm.Phase, _ string, done, total int64) {
		l.prog.set(done, total)
		if progress != nil {
			progress(done, total)
		}
	})
	if err != nil {
		return userError(ctx, err)
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
		return gterr.Invalid("取得元が応答しません（HTTP %d）。ポーズ集のリポジトリが移動または削除された可能性があります", he.Status)
	case errors.Is(err, store.ErrNotWritable):
		return gterr.Invalid("mam.exe のあるフォルダに書き込めません。書き込める場所に置いてください")
	}
	return gterr.Invalid("ポーズ集を取得できませんでした: %v", err)
}

// Poses lists the poses of the installed library (ErrNotInstalled when it is not installed). The suggestive series (sexy, duo) are in the list only
// while the age gate is open and their asset set is installed: with the gate closed the list is the one of a library that never had them.
func (l *Library) Poses() ([]Pose, error) {
	if len(l.install.NotReady()) > 0 {
		return nil, ErrNotInstalled
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.index == nil {
		idx, err := readIndex(l.dir(), l.adultDir())
		if err != nil {
			return nil, err
		}
		l.index = idx
		l.general = slices.DeleteFunc(slices.Clone(idx), func(p Pose) bool { return p.Adult })
	}
	if l.adultOpen() {
		return l.index, nil
	}
	return l.general, nil
}

// Pose is the pose with the id ("P001"), or false.
func (l *Library) Pose(id string) (Pose, bool) {
	all, err := l.Poses()
	if err != nil {
		return Pose{}, false
	}
	for _, p := range all {
		if p.ID == id {
			return p, true
		}
	}
	return Pose{}, false
}

// ImageOwner is the pose a picture belongs to, with its angle and kind (false for an ID that is not a picture of an installed pose).
func (l *Library) ImageOwner(id string) (Pose, Angle, Kind, bool) {
	all, err := l.Poses()
	if err != nil {
		return Pose{}, "", "", false
	}
	for _, p := range all {
		for _, c := range Kinds {
			for a, img := range p.views(c.Kind) {
				if img == id {
					return p, a, c.Kind, true
				}
			}
		}
	}
	return Pose{}, "", "", false
}

// File is the path of the picture with the ID (<series>_<pose id>_<angle>, and ".depth" or ".normal" after it for the maps): only a picture of
// an installed pose, never a path the caller made up. A picture of a suggestive series is answered like one that does not exist while the age
// gate is closed.
func (l *Library) File(id string) (string, error) {
	if !imageID.MatchString(id) {
		return "", gterr.Invalid("ポーズ画像の ID が正しくありません: %q", id)
	}
	p, _, _, ok := l.ImageOwner(id)
	if !ok {
		if len(l.install.NotReady()) > 0 {
			return "", ErrNotInstalled
		}
		return "", gterr.Invalid("ポーズ画像 %q はポーズ集にありません", id)
	}
	root := l.dir()
	if p.Adult {
		root = l.adultDir()
	}
	name, kind := splitID(id)
	return filepath.Join(root, kind.dir(), name+imageExt), nil
}
