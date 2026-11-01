package localruntime

import (
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
)

// WeightsState sums up the weights and dependencies of a runtime.
type WeightsState string

// The states of the weights.
const (
	// WeightsNone: the package is not installed, so nothing is to be checked yet (the weights come with the install).
	WeightsNone WeightsState = "none"
	// WeightsReady: every required file is there and verified.
	WeightsReady WeightsState = "ready"
	// WeightsMissing: a required file is not there: "installed (weights missing)"; start is refused.
	WeightsMissing WeightsState = "missing"
	// WeightsUnverified: the required files are there but nobody checked their SHA-256 (or one changed since): checking them is
	// installing the weights, and start is refused until then.
	WeightsUnverified WeightsState = "unverified"
)

// AssetView is one weight or dependency with its state, where it comes from and its license (for the screen, the MCP tools and the docs).
type AssetView struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	State       rtm.AssetState          `json:"state"`
	Required    bool                    `json:"required"`
	Size        int64                   `json:"size"`
	Source      catalog.ResourceSource  `json:"source"`
	License     catalog.ResourceLicense `json:"license"`
}

// WeightsView is the weights and dependencies of a runtime.
type WeightsView struct {
	State WeightsState `json:"state"`
	// Message says the state in a sentence ("" when there is nothing to say).
	Message string      `json:"message,omitempty"`
	Assets  []AssetView `json:"assets"`
	// NotReady counts the required assets that are not verified.
	NotReady int `json:"not_ready"`
	// NeedBytes is the free space the download of what is missing needs; TotalBytes is the size of all the weights.
	NeedBytes  int64 `json:"need_bytes"`
	TotalBytes int64 `json:"total_bytes"`
	// Unclear counts the assets whose commercial use is not "yes" (the screen shows them prominently).
	Unclear int `json:"unclear"`
}

func weightsView(inst *instance, installed bool) WeightsView {
	set := inst.install.AssetSet
	w := WeightsView{State: WeightsNone, Assets: []AssetView{}, TotalBytes: set.TotalSize()}
	states := map[string]rtm.AssetStatus{}
	for _, st := range inst.install.Assets() {
		states[st.Name] = st
	}
	for _, a := range set.Assets {
		l, _ := set.License(a.License)
		st := states[a.Name].State
		if !installed {
			st = rtm.AssetMissing
		}
		w.Assets = append(w.Assets, AssetView{Name: a.Name, Description: a.Description, State: st, Required: a.Required, Size: a.Size, Source: a.Source, License: l})
		if l.CommercialUse != catalog.CommercialYes {
			w.Unclear++
		}
	}
	if !installed {
		return w
	}
	w.NeedBytes = inst.install.AssetNeedBytes()
	w.State = WeightsReady
	for _, st := range inst.install.NotReady() {
		w.NotReady++
		if st.State == rtm.AssetMissing {
			w.State = WeightsMissing
		} else if w.State != WeightsMissing {
			w.State = WeightsUnverified
		}
	}
	switch w.State {
	case WeightsMissing:
		w.Message = "導入済み（重みが不足）。「重みを導入」で、足りないファイルを取得元から取得します"
	case WeightsUnverified:
		w.Message = "重みの SHA-256 を確かめていません。「重みを導入」で確かめます（足りないファイルは取得します）"
	}
	return w
}
