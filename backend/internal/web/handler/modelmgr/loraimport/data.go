package loraimport

import (
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/domain/lorafamily"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/loraimport"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// PanelOf is the panel above the list: the form of a file that waits when staged is set, else the choose-a-file form.
func PanelOf(svc *modelmanager.Service, staged *loraimport.Staged, notice, errMsg string) view.Panel {
	p := view.Panel{
		Notice: notice, Error: errMsg, MaxBytes: lorafile.MaxBytes, MaxText: fetch.HumanBytes(uint64(lorafile.MaxBytes)),
		GentestHref: "",
	}
	if notice != "" {
		p.GentestHref = paths.GenerationTest()
	}
	if !svc.LoRAs.Available() {
		p.Unsupported = "この環境では同梱の ComfyUI が使えないため、ここから LoRA を追加できません。外部の ComfyUI を使うときは、その loras フォルダに直接置いてください。"
	}
	if staged != nil {
		s := stagedOf(*staged)
		p.Staged = &s
	}
	return p
}

func stagedOf(s loraimport.Staged) view.Staged {
	m := s.Meta
	out := view.Staged{
		Token: s.Token, OriginalName: s.OriginalName, Name: s.Name, Renamed: s.Renamed, SavedAs: s.Name, Size: lorafile.SizeText(s.Size), SHA256: s.SHA256[:16],
		Title: m.Title, BaseRaw: m.BaseRaw, Families: familiesOf(m.BaseModels), FromFile: len(m.BaseModels) > 0,
		Triggers: strings.Join(m.TriggerWords, ", "), Candidates: m.TagCandidates, License: m.License,
	}
	if m.NetworkDim != "" || m.NetworkAlpha != "" {
		out.Network = "学習の設定: dim " + orDash(m.NetworkDim) + " / alpha " + orDash(m.NetworkAlpha)
	}
	if c := s.Conflict; c != nil {
		out.Conflict = &view.Conflict{Name: c.Name, Same: c.Same, Suggest: c.Suggest}
		out.Name = c.Suggest
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// familiesOf is every family, with the ones in keys ticked.
func familiesOf(keys []string) []view.Family {
	out := make([]view.Family, 0, len(lorafamily.All()))
	for _, o := range lorafamily.All() {
		out = append(out, view.Family{Key: o.Key, Label: o.Label, Checked: contains(keys, o.Key)})
	}
	return out
}

func contains(keys []string, k string) bool {
	for _, x := range keys {
		if x == k {
			return true
		}
	}
	return false
}

// ListOf is the list of the LoRAs that were added.
func ListOf(svc *modelmanager.Service, notice, errMsg string) view.List {
	l := view.List{Notice: notice, Error: errMsg}
	for _, m := range svc.LoRAs.List() {
		l.Items = append(l.Items, itemOf(m))
	}
	return l
}

func itemOf(m loraimport.Model) view.Item {
	it := view.Item{
		Path: m.Path, Name: m.Name, Size: lorafile.SizeText(m.Size), SHA256: m.SHA256, Present: m.Present, Families: familiesOf(m.BaseModels),
		Triggers: strings.Join(m.TriggerWords, ", "), LicenseName: m.LicenseName, CommercialV: m.Commercial,
		License: commercial.FromInfo(modellicense.FromUser(m.LicenseName, modellicense.Commercial(m.Commercial))), AddedAt: m.AddedAt.Local().Format(time.DateTime),
	}
	if m.Strength != nil {
		it.Strength = strconv.FormatFloat(*m.Strength, 'f', -1, 64)
	}
	return it
}

// listOOB is the list drawn out of band, beside the panel an action answers with.
func listOOB(svc *modelmanager.Service) templ.Component {
	return view.ListOOB(ListOf(svc, "", ""))
}
