package modelmanager

import (
	"errors"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/adult"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// The notice a user agrees to before adult models are shown (spec/architecture/model-manager/model-manager.md §6.2). A change of the
// wording is a new AdultNoticeVersion: the confirmation of the old wording no longer opens the gate, and it is asked again.
const (
	AdultNoticeVersion = "1"
	AdultNotice        = "成人向け・ローカル利用専用です。未成年（未成年に見える表現を含む）のコンテンツは生成しないでください。実在人物の同意のない性的・なりすまし画像は生成しないでください。各モデルのライセンス（Qwen 系は Research License で商用不可、Flux.1 Dev は非商用など）に従ってください。"
)

// ErrAdultNotConfirmed means the model is for adults and the age confirmation is not on record (or "show adult models" is off).
// The Service refuses it in Catalog (the model is not listed), Model, AcceptLicense, InstallModel and the lists of the trial
// generation, so no caller can skip the check by going around the screen.
var ErrAdultNotConfirmed = errors.New("modelmanager: adult models need the age confirmation")

// AdultConfirmation is what the user said when turning "show adult models" on: that they are 18 or over (or of age where they
// live) and that they agree to the notice, whose version is NoticeVersion (AdultContentStatus.NoticeVersion).
type AdultConfirmation struct {
	Adult         bool   `json:"adult"`
	NoticeVersion string `json:"notice_version"`
}

// AdultContentStatus is the age gate for adult models.
type AdultContentStatus struct {
	// Enabled: adult models are shown and can be installed (the gate is open).
	Enabled bool `json:"enabled"`
	// Confirmed: the age confirmation of the current notice is on record. Turning Enabled on needs no new confirmation then.
	Confirmed   bool       `json:"confirmed"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
	// NoticeVersion and Notice are the current wording the user has to be shown and agree to.
	NoticeVersion string `json:"notice_version"`
	Notice        string `json:"notice"`
}

// AdultContent is the state of the age gate.
func (s *Service) AdultContent() AdultContentStatus {
	out := AdultContentStatus{NoticeVersion: AdultNoticeVersion, Notice: AdultNotice}
	if rec := s.state.Get().AdultContent; rec != nil && rec.NoticeVersion == AdultNoticeVersion {
		out.Confirmed, out.Enabled = true, rec.Enabled
		at := rec.ConfirmedAt
		out.ConfirmedAt = &at
	}
	return out
}

// AdultContentEnabled reports whether the gate is open: the age is confirmed (in the current wording) and "show adult
// models" is on. Everything that offers or fetches an adult model asks this (the model manager here, and the import from
// Hugging Face of Issue #842).
func (s *Service) AdultContentEnabled() bool { return s.AdultContent().Enabled }

// SetAdultContent turns "show adult models" on or off. Turning it on needs the confirmation c (Adult true, the current
// NoticeVersion; anything else is *InvalidError) unless the age was confirmed in the current wording before (c may be nil
// then; without either it is ErrAdultNotConfirmed). The confirmation is recorded with its time and the wording version in
// state.json. Turning it off keeps the record. Nothing here confirms on the user's behalf: a caller (the screen, the MCP
// tool) passes a confirmation only after the user gave it.
func (s *Service) SetAdultContent(on bool, c *AdultConfirmation) error {
	if !on {
		if s.state.Get().AdultContent == nil {
			return nil
		}
		return s.state.Update(func(st *store.State) { st.AdultContent.Enabled = false })
	}
	if c == nil && !s.AdultContent().Confirmed {
		return ErrAdultNotConfirmed
	}
	if c != nil {
		switch {
		case !c.Adult:
			return invalid("成人向けモデルを使うには、18 歳以上（またはお住まいの地域で成年）であることの確認が必要です")
		case c.NoticeVersion != AdultNoticeVersion:
			return invalid("確認した注意書きの版が違います（%s が必要です）。画面を読み込み直してください", AdultNoticeVersion)
		}
	}
	return s.state.Update(func(st *store.State) {
		if c != nil || st.AdultContent == nil {
			st.AdultContent = &store.AdultContent{NoticeVersion: AdultNoticeVersion, ConfirmedAt: s.Now().UTC()}
		}
		st.AdultContent.Enabled = true
	})
}

// allowed reports whether the model may be listed, accepted and installed now: an adult model needs the open gate.
func (s *Service) allowed(m catalog.Model) bool { return !m.Adult() || s.AdultContentEnabled() }

// HidesModel reports whether the model with this identifier belongs to an adult model while the gate is closed: the age
// gate's one predicate, asked by the Model Adapter's Gate (every generation), the assignment choices and the trial generation
// lists, and by the run of a saved workflow (spec/architecture/model-manager/model-manager.md §6.1). A model is an adult one when
//   - it is a model of the bundled endpoint (comfyui:bundled-comfyui/<name>) that the model manager created for an adult model
//     (the catalog's adult workflows, the installed adult models' records, the NFAA models added from Hugging Face), or
//   - its ComfyUI workflow reads a file only an adult model has (adult.Files): a model of any endpoint, a model made from an
//     imported graph or under another name, and a saved workflow (comfy:<id>@v<N>) alike.
//
// A built-in model, a model of an OpenAI-compatible server and a workflow that reads only files of general models are never hidden.
func (s *Service) HidesModel(id string) bool {
	if s.AdultContentEnabled() {
		return false
	}
	st := s.state.Get()
	if kind, endpoint, name, ok := localcfg.ParseID(id); ok && kind == localcfg.KindComfyUI && endpoint == BundledEndpoint && adult.Workflows(s.cat, st)[name] {
		return true
	}
	if s.Adapter == nil {
		return false
	}
	m, _, ok := s.Adapter.Config().LocalModel(domain.ModelName(id))
	return ok && m.Workflow != nil && adult.FilesOf(s.cat, st).Reads(m.Workflow.Graph)
}

// consentKey is the key of the license acceptance in state.json: the license ID, so that one acceptance covers the models
// that share a license. An adult model is accepted on its own (key "adult:<model id>"): its consent also carries the notice
// for adult models and the source of its files, which another model with the same license (Apache-2.0, FLUX.1 [dev]) did not show.
func consentKey(m catalog.Model) string {
	if m.Adult() {
		return "adult:" + m.ID
	}
	return m.License.ID
}
