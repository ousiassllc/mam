package gentest

import (
	"slices"
	"strconv"
	"strings"
)

// The roles of an input image as the form posts them (role_<ref>): the same values as the service's roles and
// localcfg.Role* (the handler tests pin that).
const (
	RoleReference  = "reference"
	RoleFace       = "face"
	RoleStyle      = "style"
	RoleEdit       = "edit"
	RoleFirstFrame = "first_frame"
	RoleLastFrame  = "last_frame"
)

// The default strengths the sliders start at (the same as localcfg.DefaultReferenceStrength and DefaultLoRAStrength).
const (
	DefaultRefStrength  = 0.7
	DefaultLoRAStrength = 0.4
)

// Form fields of the reference options besides FieldRolePrefix (the handlers read the same names).
const (
	FieldStrengthPrefix = "strength_"
	FieldAvoidPose      = "avoid_pose"
	FieldLoRA           = "lora"
	FieldLoRAStrength   = "lora_strength"
)

// UploadPlaceholder is the value of the ref field of an image that was chosen in the browser: its file travels in the field
// upload, and "#<n>" says it is the n-th file of that field (the browser renumbers them as images are removed or moved).
const UploadPlaceholder = "upload::#"

type roleOption struct{ Value, Label string }

// imageRoles are the roles of a reference of an image model, in the order the select lists them.
var imageRoles = []roleOption{{RoleReference, "全体"}, {RoleFace, "顔だけ"}, {RoleStyle, "画風だけ"}, {RoleEdit, "編集対象"}}

var videoRoles = []roleOption{{RoleReference, "参照"}, {RoleFirstFrame, "最初のフレーム"}, {RoleLastFrame, "最後のフレーム"}}

// RefUI is how the controls of an input image are drawn: Roles are the roles the chosen models take (none: no chosen model
// takes input images, so the role and strength controls are hidden).
type RefUI struct{ Roles []string }

// Show says the role and the strength are offered.
func (u RefUI) Show() bool { return len(u.Roles) > 0 }

// Offers says the role is one the chosen models take.
func (u RefUI) Offers(role string) bool { return slices.Contains(u.Roles, role) }

// chosen are the models that are ticked and can be called.
func (c Compose) chosen() []ModelChoice {
	var out []ModelChoice
	for _, m := range c.Models {
		if m.Selected && m.Reason == "" {
			out = append(out, m)
		}
	}
	return out
}

// RefUI is the controls for the chosen models of the form (RefUIOf).
func (c Compose) RefUI() RefUI { return RefUIOf(c.Models) }

// RefUIOf is the controls for the models that are ticked and can be called: the union of the roles they take, in the order of
// the select.
func RefUIOf(models []ModelChoice) RefUI {
	var u RefUI
	for _, o := range imageRoles {
		if slices.ContainsFunc(models, func(m ModelChoice) bool { return m.Selected && m.Reason == "" && slices.Contains(m.RefRoles, o.Value) }) {
			u.Roles = append(u.Roles, o.Value)
		}
	}
	return u
}

// CanLoRA says a LoRA can be chosen: a chosen model takes one and the engine has some.
func (c Compose) CanLoRA() bool {
	return len(c.LoRAs) > 0 && slices.ContainsFunc(c.chosen(), func(m ModelChoice) bool { return m.CanLoRA })
}

// NoRefNotice is the notice that a chosen model does not use the input images ("" when there is none to say).
func (c Compose) NoRefNotice() string {
	if len(c.Picked) == 0 || c.Media == "video" {
		return ""
	}
	var names []string
	for _, m := range c.chosen() {
		if len(m.RefRoles) == 0 {
			names = append(names, m.Label)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, "、") + " は入力画像を使いません"
}

// ModeLabel is the short word of how a model uses input images ("" for one that says none).
func (m ModelChoice) ModeLabel() string {
	switch m.RefMode {
	case "edit":
		return "編集"
	case "img2img":
		return "画像から"
	}
	return ""
}

// RefHint is the tooltip of the input image icon of a model card.
func (m ModelChoice) RefHint() string {
	switch {
	case len(m.RefRoles) == 0:
		return "入力画像は使えません"
	case m.RefMode == "edit":
		return "入力画像: 編集（複数枚・役割つき）"
	case m.RefMode == "img2img":
		return "入力画像: 画像から（1 枚を元に生成）"
	}
	return "入力画像を使えます"
}

// strengthText is a strength as the slider shows it ("0.70").
func strengthText(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// strengthValue is a strength as the slider's value attribute: the default when none was given.
func strengthValue(v, def float64) string {
	if v <= 0 {
		v = def
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// strengthOrDefault is the value of a slider that was not moved.
func strengthOrDefault(v, def float64) float64 {
	if v <= 0 {
		return def
	}
	return v
}
