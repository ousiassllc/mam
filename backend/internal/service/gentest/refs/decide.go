package refs

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// The codes of a Warning of Decide (the codes of package gentest's Warning).
const (
	WarnInputsDropped   = "inputs_dropped"   // the model takes no reference image
	WarnTooMany         = "too_many_refs"    // more reference images than the model takes
	WarnRoleUnsupported = "role_unsupported" // the model does not take the role: the picture is used as a whole
	WarnLoRAUnsupported = "lora_unsupported" // the model cannot take a LoRA: it is not sent
)

// Warning is something Decide tells the user about one model.
type Warning struct{ Code, Message string }

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

var roleLabel = map[string]string{"face": "顔", "style": "スタイル", "edit": "編集"}

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
		n := len(roles)
		if m.MaxReferences > 0 {
			n = min(n, m.MaxReferences)
		}
		for i := range n {
			d.Used = append(d.Used, i)
		}
		if n < len(roles) {
			warn(WarnTooMany, "%s は参照画像を %d 枚までしか使えないため、先頭の %d 枚だけ使い、残りの %d 枚は使いません", m.Label, n, n, len(roles)-n)
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
			warn(WarnRoleUnsupported, "%s は「%s」の参照に対応していないため、その画像は全体の参照として使います", m.Label, strings.Join(labels, "・"))
		}
	}
	if hasLoRA {
		if m.SupportsLoRA {
			d.LoRA = true
		} else {
			warn(WarnLoRAUnsupported, "%s は LoRA に対応していないため、LoRA は使いません", m.Label)
		}
	}
	d.AvoidPose = avoidPose && len(d.Used) > 0
	return d
}
