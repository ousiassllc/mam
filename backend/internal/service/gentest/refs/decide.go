package refs

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// The codes of a Warning of Decide (the codes of package gentest's Warning).
const (
	WarnInputsDropped     = "inputs_dropped"     // the model takes no reference image
	WarnTooMany           = "too_many_refs"      // more reference images than the model takes
	WarnRoleUnsupported   = "role_unsupported"   // the model does not take the role: the picture is used as a whole
	WarnMarkupUnsupported = "markup_unsupported" // the model does not take a markup image: it is not sent (a request with one is refused before it runs)
	WarnLoRAUnsupported   = "lora_unsupported"   // the model cannot take a LoRA: it is not sent
)

// Warning is something Decide tells the user about one model. Roles is only for WarnRoleUnsupported: the roles (face / style /
// edit) of the references that the model does not take and so uses as a whole, in the order the references give them.
type Warning struct {
	Code, Message string
	Roles         []string
}

// Model is what Decide needs to know of an image model (modelcatalog.Info).
type Model struct {
	Label string
	// AcceptsReferences: the model takes reference images at all.
	AcceptsReferences bool
	// MaxReferences is how many it takes (0: not known, all of them are sent).
	MaxReferences int
	// Roles are the roles it takes (empty: the whole picture only).
	Roles        []string
	SupportsLoRA bool
	// MarkupRefusal is why the model takes no markup image, when it has a reason of its own (the ChatGPT-subscription path); "" for the plain sentence.
	MarkupRefusal string
}

// Decision is how a model takes the references, the LoRA and the pose option of a request.
type Decision struct {
	// Used are the indexes of the request's references the model is given, in order.
	Used []int
	// LoRA says the LoRA is sent to the model.
	LoRA bool
	// AvoidPose says the negative prompt of the model gets localcfg.AvoidPoseNegative: only when it uses a reference.
	AvoidPose bool
	Warnings  []Warning
}

var roleLabel = map[string]string{"face": "顔", "style": "スタイル", "edit": "編集", "pose": "ポーズ", "markup": "マークアップ"}

// MarkupUnsupported is the sentence for a model that takes no markup image (refusal is the model's own reason, "" for none).
func MarkupUnsupported(label, refusal string) string {
	if refusal != "" {
		return label + ": " + refusal
	}
	return label + " はマークアップに対応していません（GPT Image 2.6 の API キー経由だけ）"
}

// Decide plans one image model: roles are the roles of the request's references, hasLoRA and avoidPose its options.
func Decide(m Model, roles []string, hasLoRA, avoidPose bool) Decision {
	var d Decision
	warn := func(code, format string, args ...any) {
		d.Warnings = append(d.Warnings, Warning{Code: code, Message: fmt.Sprintf(format, args...)})
	}
	switch {
	case len(roles) == 0:
	case !m.AcceptsReferences:
		warn(WarnInputsDropped, "%s は参照画像を使えないため、プロンプトだけで生成します", m.Label)
	default:
		// A markup image is laid over another picture, never used as one: a model that cannot take it is not given it (and says so).
		markupOK := slices.Contains(m.Roles, gentestrepo.RoleMarkup)
		var candidates []int
		markupSaid := false
		for i, r := range roles {
			switch {
			case r != gentestrepo.RoleMarkup || markupOK:
				candidates = append(candidates, i)
			case !markupSaid:
				markupSaid = true
				warn(WarnMarkupUnsupported, "%s", MarkupUnsupported(m.Label, m.MarkupRefusal))
			}
		}
		n := len(candidates)
		if m.MaxReferences > 0 {
			n = min(n, m.MaxReferences)
		}
		d.Used = candidates[:n]
		if n < len(candidates) {
			warn(WarnTooMany, "%s は参照画像を %d 枚までしか使えないため、先頭の %d 枚だけ使い、残りの %d 枚は使いません", m.Label, n, n, len(candidates)-n)
		}
		var unsupported []string
		for _, i := range d.Used {
			if r := roles[i]; r != localcfg.RoleWhole && !slices.Contains(m.Roles, r) && !slices.Contains(unsupported, r) {
				unsupported = append(unsupported, r)
			}
		}
		if len(unsupported) > 0 {
			labels := make([]string, len(unsupported))
			for i, r := range unsupported {
				labels[i] = roleLabel[r]
			}
			d.Warnings = append(d.Warnings, Warning{
				Code:    WarnRoleUnsupported,
				Message: fmt.Sprintf("%s は「%s」の参照に対応していないため、その画像は全体の参照として使います", m.Label, strings.Join(labels, "・")),
				Roles:   unsupported,
			})
		}
	}
	if hasLoRA {
		if m.SupportsLoRA {
			d.LoRA = true
		} else {
			warn(WarnLoRAUnsupported, "%s は LoRA に対応していないため、LoRA は使いません", m.Label)
		}
	}
	// A pose reference is there to be copied: the option is about the other pictures (a pose chart the model reads as a pose is not one).
	d.AvoidPose = avoidPose && slices.ContainsFunc(d.Used, func(i int) bool {
		return roles[i] != localcfg.RolePose || !slices.Contains(m.Roles, localcfg.RolePose)
	})
	return d
}
