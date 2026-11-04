package poses

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// AdultSetID is the id of the asset set of the suggestive series (sexy, duo; data/assets/shisei-poses-adult.json) and the name of its directory below
// localai. It holds only pictures: their names and categories are in the pose catalog of the main set, so that set has to be installed first.
const AdultSetID = "shisei-poses-adult"

// ErrAdultGateClosed is the refusal of an install of the suggestive series while the age gate is closed: nothing is downloaded, and the user
// is told where the gate is opened (model manager; the same switch as for the adult models, spec model-manager.md §6.2). It says nothing of what
// the series holds.
var ErrAdultGateClosed = gterr.Invalid("成人向けのポーズ集は、モデル管理で年齢確認（成人向けモデルを表示する）をオンにするまで導入できません")

// ErrBaseNotInstalled is the refusal of an install of the suggestive series before the main library: it names the poses.
var ErrBaseNotInstalled = gterr.Invalid("先に、ポーズ集（全年齢向け）を導入してください")

// adultSet is the asset set of the suggestive series with its own installer (its own directory, record and downloads).
type adultSet struct {
	set     catalog.ResourceSet
	layout  store.RuntimeLayout
	install *rtm.Installer
}

// newAdultSet opens the asset set of the suggestive series below d.Root; nil when the catalog has none (the library then has only the main set).
func newAdultSet(d Deps) (*adultSet, error) {
	set, ok := d.Catalog.ResourceSet(AdultSetID)
	if !ok {
		return nil, nil
	}
	layout := store.ForRuntime(d.Root, AdultSetID)
	st, err := store.OpenRuntime(layout)
	if err != nil {
		return nil, err
	}
	return &adultSet{set: set, layout: layout, install: &rtm.Installer{Layout: layout, AssetSet: set, Store: st, Fetch: d.Fetch, FreeBytes: d.FreeBytes}}, nil
}

// adultOpen reports whether the age gate is open now. It is asked on every call, so turning the age confirmation on or off applies at once.
func (l *Library) adultOpen() bool { return l.open != nil && l.open() }

// adultDir is where the pictures of the suggestive series are: "" while they are not usable (no such set, or not installed).
func (l *Library) adultDir() string {
	if l.adult == nil || len(l.adult.install.NotReady()) > 0 {
		return ""
	}
	return l.adult.layout.EnvDir()
}

// Hides reports whether the picture with the ID is one the age gate keeps out of sight now: one of a suggestive series while the gate is closed.
// Whoever shows something that came from a picture (an input image of a result, a label) asks it, so that the name and the image of such a pose
// do not show anywhere while the gate is closed.
func (l *Library) Hides(pictureID string) bool { return IsAdultImage(pictureID) && !l.adultOpen() }

// AdultStatus is the offer of the suggestive series: what the install downloads, and whether it is installed.
type AdultStatus struct {
	Info
	Installed bool
}

// adultStatus is nil while the age gate is closed or when there is no such set: the offer is not made, nothing of the series is said.
func (l *Library) adultStatus() *AdultStatus {
	if l.adult == nil || !l.adultOpen() {
		return nil
	}
	return &AdultStatus{Info: infoOf(l.adult.set, l.adult.install), Installed: len(l.adult.install.NotReady()) == 0}
}

// InstallAdult downloads and unpacks the suggestive series (sexy, duo: line art, depth maps and normal maps) like Install does the main set. It
// refuses at once, without fetching, while the age gate is closed (ErrAdultGateClosed: the age is never taken as confirmed), when accepted is
// false, or when the main library is not installed.
func (l *Library) InstallAdult(ctx context.Context, accepted bool, progress func(done, total int64)) error {
	switch {
	case l.adult == nil || !l.adultOpen():
		return ErrAdultGateClosed
	case !accepted:
		return ErrNotAccepted
	case len(l.install.NotReady()) > 0:
		return ErrBaseNotInstalled
	}
	return l.ensure(ctx, l.adult.install, progress)
}

// RemoveAdult deletes the suggestive series only (the main library stays). A series that is not installed is removed without an error; a running
// install is a conflict. Like Remove it is allowed whatever the age gate says.
func (l *Library) RemoveAdult() error {
	if l.prog.running.Load() {
		return gterr.Conflict("ポーズ集を導入している最中は削除できません。終わってからやり直してください")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.index, l.general = nil, nil
	if l.adult == nil {
		return nil
	}
	return l.adult.install.Remove()
}
