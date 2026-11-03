// Package facefusion runs FaceFusion (https://github.com/facefusion/facefusion), the face swapper, as an external tool of mam in the way
// ffmpeg is one (spec/architecture/gentest/face-swap.md): mam bundles neither FaceFusion nor its models, the user installs it, and mam
// only finds it (Look) and starts it as a separate process with `facefusion.py headless-run` (Swap). The licenses of the models are
// the ones the FaceFusion documentation lists (https://docs.facefusion.io/introduction/licenses); a model that list does not clear for
// commercial use is shown with a warning, and one it does not settle is shown as "commercial use unknown".
package facefusion

import (
	"fmt"
	"slices"
	"strings"
)

// LicensesURL is the page of the FaceFusion documentation that lists the license of every model it uses (the primary source of Models).
const LicensesURL = "https://docs.facefusion.io/introduction/licenses"

// DisclaimerURL is FaceFusion's disclaimer: it refuses nude and sexual content, and its content analyser cannot be switched off.
const DisclaimerURL = "https://docs.facefusion.io/introduction/disclaimer"

// Class says what the license of a model allows, in the terms mam shows.
type Class string

// The classes. Open is a license that allows commercial use as it stands (Apache-2.0, MIT, BSD); Conditional allows it under conditions
// that must be read (OpenRAIL-M, GPL-3.0); NonCommercial and Research do not allow it; Unknown is a model whose license the
// documentation lists as none or unknown, or does not list.
const (
	ClassOpen          Class = "open"
	ClassConditional   Class = "conditional"
	ClassNonCommercial Class = "noncommercial"
	ClassResearch      Class = "research"
	ClassUnknown       Class = "unknown"
)

// Text is the class in words, as the screens and the tools say it.
func (c Class) Text() string {
	switch c {
	case ClassOpen:
		return "商用の制限なし"
	case ClassConditional:
		return "商用は条件つき"
	case ClassNonCommercial:
		return "商用NG"
	case ClassResearch:
		return "研究用のみ"
	}
	return "商用不明"
}

// Model is one model FaceFusion can be told to use. ID is the value of the option (`--face-swapper-model` and the like).
type Model struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// License is the license as docs.facefusion.io/introduction/licenses lists it for the model ("Apache 2.0", "Non-Commercial", ...).
	License string `json:"license"`
	Class   Class  `json:"class" jsonschema:"open / conditional / noncommercial / research / unknown"`
	// Note is what else the license needs to be read with (a second model it brings, a conflict between the sources); "" for none.
	Note string `json:"note,omitempty"`
}

// Warn says the model needs a warning: its license does not allow commercial use or does not settle it.
func (m Model) Warn() bool {
	return m.Class == ClassNonCommercial || m.Class == ClassResearch || m.Class == ClassUnknown
}

// Warning is the caution for the user who chose the model, in words ("" for a model that needs none).
func (m Model) Warning() string {
	if !m.Warn() {
		return ""
	}
	text := fmt.Sprintf("%s は%s（FaceFusion の一覧: %s）。個人の試用に限り、商用の作品には使わないでください", m.ID, m.Class.Text(), m.License)
	if m.Note != "" {
		text += "。" + m.Note
	}
	return text
}

// The kinds of model of a selection.
const (
	KindSwapper  = "swapper"
	KindDetector = "detector"
	KindEnhancer = "enhancer"
)

// The defaults (Issue #995): GHOST for the swap, YuNet for the detection and GFPGAN 1.4 for the restoration, the ones whose licenses the
// FaceFusion list gives as Apache 2.0 and MIT. Which GHOST version looks best has not been compared (it needs a GPU: remaining of the Issue).
const (
	DefaultSwapper  = "ghost_2_256"
	DefaultDetector = "yunet"
	DefaultEnhancer = "gfpgan_1.4"
	// EnhancerNone runs no face restoration.
	EnhancerNone = "none"
)

const ghostNote = "顔の特徴の変換に CrossFace（OpenRAIL-M）の重みも使う。その条項は読んでから使うこと"

// Swappers are the face swapper models (`--face-swapper-model`). The names are those of FaceFusion 3.x
// (facefusion/processors/modules/face_swapper/types.py).
var Swappers = []Model{
	{ID: "ghost_1_256", Label: "GHOST 1 (256)", License: "Apache 2.0", Class: ClassConditional, Note: ghostNote},
	{ID: "ghost_2_256", Label: "GHOST 2 (256)", License: "Apache 2.0", Class: ClassConditional, Note: ghostNote},
	{ID: "ghost_3_256", Label: "GHOST 3 (256)", License: "Apache 2.0", Class: ClassConditional, Note: ghostNote},
	{ID: "inswapper_128", Label: "InSwapper 128", License: "Non-Commercial", Class: ClassNonCommercial},
	{ID: "inswapper_128_fp16", Label: "InSwapper 128 (fp16)", License: "Non-Commercial", Class: ClassNonCommercial},
	{ID: "simswap_256", Label: "SimSwap 256", License: "Non-Commercial", Class: ClassNonCommercial},
	{ID: "simswap_unofficial_512", Label: "SimSwap 512 (非公式)", License: "Non-Commercial", Class: ClassNonCommercial},
	{ID: "blendswap_256", Label: "BlendSwap 256", License: "Non-Commercial", Class: ClassNonCommercial},
	{ID: "alphaface_256", Label: "AlphaFace 256", License: "Non-Commercial", Class: ClassNonCommercial},
	{ID: "hyperswap_1a_256", Label: "HyperSwap 1a (256)", License: "ResearchRAIL", Class: ClassResearch},
	{ID: "hyperswap_1b_256", Label: "HyperSwap 1b (256)", License: "ResearchRAIL", Class: ClassResearch},
	{ID: "hyperswap_1c_256", Label: "HyperSwap 1c (256)", License: "ResearchRAIL", Class: ClassResearch},
	{ID: "hififace_unofficial_256", Label: "HiFiFace 256 (非公式)", License: "Unknown", Class: ClassUnknown},
	{ID: "uniface_256", Label: "UniFace 256", License: "None", Class: ClassUnknown, Note: "一覧は License: None と書く。ライセンスが無いことは、使ってよいという意味ではない"},
}

// Detectors are the face detector models (`--face-detector-model`).
var Detectors = []Model{
	{ID: "yunet", Label: "YuNet", License: "MIT", Class: ClassOpen},
	{ID: "yolo_face", Label: "YOLO Face", License: "GPL-3.0", Class: ClassConditional, Note: "GPL-3.0 のコピーレフトが付く"},
	{ID: "retinaface", Label: "RetinaFace", License: "Non-Commercial", Class: ClassNonCommercial},
	{ID: "scrfd", Label: "SCRFD", License: "Non-Commercial", Class: ClassNonCommercial},
	{ID: "many", Label: "複数を併用（many）", License: "Non-Commercial", Class: ClassNonCommercial, Note: "RetinaFace・SCRFD・YOLO Face を併せて使うので、いちばん厳しいものに従う"},
}

// Enhancers are the face restoration models (`--face-enhancer-model`). EnhancerNone (no restoration) is not one of them.
var Enhancers = []Model{
	{ID: "gfpgan_1.4", Label: "GFPGAN 1.4", License: "Apache 2.0", Class: ClassOpen},
	{ID: "gfpgan_1.3", Label: "GFPGAN 1.3", License: "Apache 2.0", Class: ClassOpen},
	{ID: "gfpgan_1.2", Label: "GFPGAN 1.2", License: "Apache 2.0", Class: ClassOpen},
	{ID: "restoreformer_plus_plus", Label: "RestoreFormer++", License: "Apache 2.0", Class: ClassOpen},
	{ID: "codeformer", Label: "CodeFormer", License: "S-Lab 1.0", Class: ClassNonCommercial, Note: "S-Lab License は非商用が基本で、商用は作者への連絡が要る"},
	{ID: "gpen_bfr_256", Label: "GPEN BFR 256", License: "Non-Commercial", Class: ClassNonCommercial},
	{ID: "gpen_bfr_512", Label: "GPEN BFR 512", License: "Non-Commercial", Class: ClassNonCommercial},
	{ID: "gpen_bfr_1024", Label: "GPEN BFR 1024", License: "Non-Commercial", Class: ClassNonCommercial},
	{ID: "gpen_bfr_2048", Label: "GPEN BFR 2048", License: "Non-Commercial", Class: ClassNonCommercial},
}

// ArcFaceNote is the license caution that holds for every swap whatever models are chosen: FaceFusion measures the faces of the picture
// and of the source with ArcFace (InsightFace), which its list gives as Non-Commercial, and no option replaces it (facefusion/face_recognizer.py
// has that one model).
const ArcFaceNote = "FaceFusion は、どのモデルを選んでも、顔の特徴の計算に ArcFace（InsightFace 由来。FaceFusion の一覧では Non-Commercial）を使います。選べないので、既定のモデルでも商用の作品への利用は確かめられません"

// Selection is the three models of a swap. An empty field is the default; Enhancer EnhancerNone runs no restoration.
type Selection struct {
	Swapper  string `json:"swapper"`
	Detector string `json:"detector"`
	Enhancer string `json:"enhancer"`
}

// WithDefaults fills the empty fields.
func (s Selection) WithDefaults() Selection {
	s.Swapper = cmpOr(s.Swapper, DefaultSwapper)
	s.Detector = cmpOr(s.Detector, DefaultDetector)
	s.Enhancer = cmpOr(s.Enhancer, DefaultEnhancer)
	return s
}

func cmpOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// Resolve checks the selection (after WithDefaults) and returns the models it names: the enhancer is nil for EnhancerNone. An
// unknown id is an error that lists the ones that exist.
func (s Selection) Resolve() (swapper, detector Model, enhancer *Model, err error) {
	s = s.WithDefaults()
	if swapper, err = find(KindSwapper, Swappers, s.Swapper); err != nil {
		return
	}
	if detector, err = find(KindDetector, Detectors, s.Detector); err != nil {
		return
	}
	if s.Enhancer == EnhancerNone {
		return
	}
	e, err := find(KindEnhancer, Enhancers, s.Enhancer)
	if err != nil {
		return
	}
	return swapper, detector, &e, nil
}

func find(kind string, list []Model, id string) (Model, error) {
	i := slices.IndexFunc(list, func(m Model) bool { return m.ID == id })
	if i >= 0 {
		return list[i], nil
	}
	ids := make([]string, len(list))
	for i, m := range list {
		ids[i] = m.ID
	}
	if kind == KindEnhancer {
		ids = append(ids, EnhancerNone)
	}
	return Model{}, fmt.Errorf("%s %q は選べません（%s）", kindText[kind], id, strings.Join(ids, " / "))
}

var kindText = map[string]string{KindSwapper: "入れ替えモデル", KindDetector: "顔検出モデル", KindEnhancer: "顔の復元モデル"}

// Warnings are the cautions for the user who chose the selection: one for each model that needs one (Model.Warning). Empty for a selection of
// models whose licenses are cleared; ArcFaceNote is separate, since it holds for every selection.
func (s Selection) Warnings() ([]string, error) {
	swapper, detector, enhancer, err := s.Resolve()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range []*Model{&swapper, &detector, enhancer} {
		if m != nil && m.Warn() {
			out = append(out, m.Warning())
		}
	}
	return out, nil
}

// AdultNote is shown wherever a swap is offered: FaceFusion declines nude and sexual pictures, and its NSFW check (the content analyser)
// cannot be switched off (https://docs.facefusion.io/introduction/disclaimer), so it is not a tool for the results of mam's adult models.
const AdultNote = "FaceFusion は、ヌード・成人向けの画像の処理を断ります（公式の方針で、NSFW の判定は無効にできません）。mam の成人向けモデルで作った画像の顔の入れ替えには使えません"

// LicenseNote is what FaceFusion itself is licensed under: OpenRAIL-AS since 2025-02 (it was MIT before), with use restrictions that bind
// the user. mam has not read its clauses in full (the repository has no full text), so it only names it.
const LicenseNote = "FaceFusion 本体のライセンスは OpenRAIL-AS（2025-02 に MIT から変更）です。利用の制限が使う人に直接かかります。条項は使う前に読んでください"

// FirstRunNote says what FaceFusion does by itself the first time a model is used: it fetches the model files, not mam.
const FirstRunNote = "初めて使うモデルは、FaceFusion 自身が配布元から取得します（mam は取得しません）。初回は時間がかかり、ディスクを使います"
