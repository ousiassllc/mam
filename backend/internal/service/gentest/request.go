package gentest

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/refs"

	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// Where an input image comes from (RefInput.Source).
const (
	// FromStaged: an image uploaded to the screen or by an MCP upload ticket (StageReference); ID is its staged id.
	FromStaged = "upload"
	// FromProjectAsset: an image asset of a Project (ProjectID and the asset's ID).
	FromProjectAsset = "project_asset"
	// FromReferenceItem: an entry of a Project's Reference Library (ProjectID and the entry's ID).
	FromReferenceItem = "reference_item"
	// FromHistory: the file of a succeeded image result of the history (the result's ID).
	FromHistory = "history"
)

// PromptInput is a prompt with its optional negative prompt.
type PromptInput struct {
	Prompt   string `json:"prompt"`
	Negative string `json:"negative_prompt,omitempty"`
}

// RefInput is an input image of a batch and the role it plays. Role is reference (the whole picture: a reference image of an
// image model, a free reference of a video model), face, style or edit (image models that take them, see
// modelcatalog.Info.ReferenceRoles), first_frame or last_frame (video). Strength (0..1, nil: localcfg.DefaultReferenceStrength)
// is how strongly an image model shows the reference.
type RefInput struct {
	Source    string   `json:"source"`
	ID        string   `json:"id"`
	ProjectID string   `json:"project_id,omitempty"`
	Role      string   `json:"role,omitempty"`
	Strength  *float64 `json:"strength,omitempty"`
}

// LoRAInput is a LoRA of the engine (its file name in the loras folder; modelcatalog.Catalog lists them) and its strength
// (nil: localcfg.DefaultLoRAStrength).
type LoRAInput struct {
	Name     string   `json:"name"`
	Strength *float64 `json:"strength,omitempty"`
}

// Request is a batch: every model x every prompt x Count results.
type Request struct {
	Media  string   `json:"media"`
	Models []string `json:"models"`
	// Prompts: at least one; the same (prompt, negative prompt) twice counts once.
	Prompts []PromptInput `json:"prompts"`
	// Count is the results per model and prompt (default 1).
	Count int `json:"count,omitempty"`
	// AspectRatio is "W:H" (default 1:1 for an image, 16:9 for a video).
	AspectRatio string `json:"aspect_ratio,omitempty"`
	// Resolution and DurationSeconds are for video. Empty / 0 take 720p (or the model's first resolution) and 5 seconds, each
	// snapped to what the model makes.
	Resolution      string     `json:"resolution,omitempty"`
	DurationSeconds float64    `json:"duration_seconds,omitempty"`
	References      []RefInput `json:"references,omitempty"`
	// AvoidReferencePose adds localcfg.AvoidPoseNegative to the negative prompt of every image model that takes references,
	// so that the pose and the composition of a reference are not copied.
	AvoidReferencePose bool `json:"avoid_reference_pose,omitempty"`
	// LoRA is applied to every image model that can take one (modelcatalog.Info.SupportsLoRA), together with the references.
	LoRA *LoRAInput `json:"lora,omitempty"`
	// TranslateToEnglish translates the prompts and the negative prompts that are not English with the Reasoning LLM before
	// they are sent (once per run; English ones are left as they are). A failed translation stops the run. The zero value is
	// off: DefaultTranslate is what the screen and the MCP tools ask for when the caller does not say.
	TranslateToEnglish bool `json:"translate_to_english"`
}

var aspectPattern = regexp.MustCompile(`^[1-9][0-9]?:[1-9][0-9]?$`)

// defaultSeconds is the length asked of a video model that was not given one.
const defaultSeconds = 5.0

// normalize checks the request against the media and the models of this setting and fills the defaults. It does not look at
// the inputs' files (resolveInputs does) and does not price anything.
func (s *Service) normalize(r Request) (Request, []modelcatalog.Info, error) {
	r.Media = strings.TrimSpace(r.Media)
	if r.Media != gentestrepo.MediaImage && r.Media != gentestrepo.MediaVideo {
		return r, nil, gterr.Invalid("画像か動画かを選んでください")
	}
	models, err := s.chosenModels(r)
	if err != nil {
		return r, nil, err
	}
	if r.Prompts, err = cleanPrompts(r.Prompts); err != nil {
		return r, nil, err
	}
	if r.Count == 0 {
		r.Count = 1
	}
	if r.Count < 1 || r.Count > MaxCount {
		return r, nil, gterr.Invalid("本数は 1〜%d にしてください", MaxCount)
	}
	if r.AspectRatio = strings.TrimSpace(r.AspectRatio); r.AspectRatio == "" {
		r.AspectRatio = map[string]string{gentestrepo.MediaImage: "1:1", gentestrepo.MediaVideo: "16:9"}[r.Media]
	}
	if !aspectPattern.MatchString(r.AspectRatio) {
		return r, nil, gterr.Invalid("縦横比は 16:9 のように指定してください")
	}
	if r.Media == gentestrepo.MediaImage && (r.Resolution != "" || r.DurationSeconds != 0) {
		return r, nil, gterr.Invalid("解像度と長さは動画だけで指定できます")
	}
	if r.DurationSeconds < 0 {
		return r, nil, gterr.Invalid("長さは 0 より大きい秒数にしてください")
	}
	if r.Media == gentestrepo.MediaVideo && r.DurationSeconds == 0 {
		r.DurationSeconds = defaultSeconds
	}
	if err := s.checkReferences(r); err != nil {
		return r, nil, err
	}
	if r.LoRA != nil {
		l, err := refs.CheckLoRA(r.Media, r.LoRA.Name, r.LoRA.Strength)
		if err != nil {
			return r, nil, err
		}
		r.LoRA = &LoRAInput{Name: l.Name, Strength: &l.Strength} // a copy: the caller's request stays as it was
	}
	if err := refs.CheckAvoidPose(r.Media, r.AvoidReferencePose); err != nil {
		return r, nil, err
	}
	if n := len(models) * len(r.Prompts) * r.Count; n > s.maxItems() {
		return r, nil, gterr.Invalid("1 回で作れるのは %d 本までです（今の組み合わせは %d 本）。モデル・プロンプト・本数を減らしてください", s.maxItems(), n)
	}
	return r, models, nil
}

func (s *Service) maxItems() int { return s.Adapter.Config().Gentest.Items() }

// chosenModels finds the requested models among the models of the media: each must exist and be available now.
func (s *Service) chosenModels(r Request) ([]modelcatalog.Info, error) {
	if len(r.Models) == 0 {
		return nil, gterr.Invalid("モデルを 1 つ以上選んでください")
	}
	have := s.catalog.Models(r.Media)
	var out []modelcatalog.Info
	for _, id := range r.Models {
		if slices.ContainsFunc(out, func(m modelcatalog.Info) bool { return m.ID == id }) {
			return nil, gterr.Invalid("モデル %s が重複しています", id)
		}
		i := slices.IndexFunc(have, func(m modelcatalog.Info) bool { return m.ID == id })
		if i < 0 {
			return nil, gterr.Invalid("モデル %q は選べません（%s のモデルの一覧にありません）", id, mediaLabel(r.Media))
		}
		if !have[i].Available {
			return nil, gterr.Invalid("モデル %s は今使えません: %s", have[i].Label, have[i].UnavailableMessage)
		}
		out = append(out, have[i])
	}
	return out, nil
}

func mediaLabel(media string) string {
	if media == gentestrepo.MediaVideo {
		return "動画"
	}
	return "画像"
}

// cleanPrompts trims the prompts, refuses an empty or a too long one and drops a repeated (prompt, negative prompt).
func cleanPrompts(in []PromptInput) ([]PromptInput, error) {
	var out []PromptInput
	for _, p := range in {
		p.Prompt, p.Negative = strings.TrimSpace(p.Prompt), strings.TrimSpace(p.Negative)
		if p.Prompt == "" && p.Negative == "" {
			continue // an empty row of the form
		}
		if p.Prompt == "" || utf8.RuneCountInString(p.Prompt) > MaxPromptRunes {
			return nil, gterr.Invalid("プロンプトは 1〜%d 文字で入力してください", MaxPromptRunes)
		}
		if utf8.RuneCountInString(p.Negative) > MaxPromptRunes {
			return nil, gterr.Invalid("ネガティブプロンプトは %d 文字以内にしてください", MaxPromptRunes)
		}
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, gterr.Invalid("プロンプトを 1 つ以上入力してください")
	}
	return out, nil
}

// checkReferences checks the roles and strengths against the media. The files are read later; a model that cannot use a role is
// a warning of the estimate, not a refusal.
func (s *Service) checkReferences(r Request) error {
	if len(r.References) > MaxReferences {
		return gterr.Invalid("入力画像は %d 枚までです", MaxReferences)
	}
	firsts, lasts := 0, 0
	for i := range r.References {
		ref := &r.References[i]
		var err error
		if ref.Role, err = refs.CheckRole(r.Media, ref.Role, ref.Strength); err != nil {
			return err
		}
		firsts += btoi(ref.Role == gentestrepo.RoleFirstFrame)
		lasts += btoi(ref.Role == gentestrepo.RoleLastFrame)
		if err := checkSource(*ref); err != nil {
			return err
		}
	}
	if firsts > 1 || lasts > 1 {
		return gterr.Invalid("最初のフレーム・最後のフレームは 1 枚ずつまでです")
	}
	return nil
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func checkSource(ref RefInput) error {
	if ref.ID == "" {
		return gterr.Invalid("入力画像の ID がありません")
	}
	switch ref.Source {
	case FromStaged, FromHistory:
	case FromProjectAsset, FromReferenceItem:
		if ref.ProjectID == "" {
			return gterr.Invalid("プロジェクトの画像には project_id が要ります")
		}
	default:
		return gterr.Invalid("入力画像の取得元が正しくありません: %q（upload / project_asset / reference_item / history）", ref.Source)
	}
	return nil
}
