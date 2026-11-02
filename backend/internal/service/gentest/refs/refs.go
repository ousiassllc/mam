// Package refs holds what the generation test does with the reference images of an image batch beyond the pictures
// themselves: the check of a role and a strength, the check of the LoRA and of the "do not copy the pose" option, and the
// decision of what one model takes of them (how many images, which roles, whether the LoRA and the pose negative reach it).
// It knows no request or model type of package gentest, which calls it.
package refs

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// MinStrength is the least strength sent to a model: localcfg.RefOption and the ComfyUI LoRA treat 0 as "not given" (the
// default), so a strength of 0 that the user chose is sent as this.
const MinStrength = 0.01

// MaxLoRANameRunes bounds the name of a LoRA (a file name of the loras folder, sub folders included).
const MaxLoRANameRunes = 200

// LoRA is the LoRA a result was made with (gentest_result.params "lora") and what a history view shows of it.
type LoRA struct {
	Name     string  `json:"name"`
	Strength float64 `json:"strength"`
}

// Sent is the LoRA as the model is given it: a strength of 0 that the user chose is MinStrength, never the default the
// engine puts in the place of 0.
func (l LoRA) Sent() localcfg.LoRA {
	return localcfg.LoRA{Name: l.Name, Strength: max(l.Strength, MinStrength)}
}

// ImageRoles are the roles a reference of an image model may have (the whole picture first).
var ImageRoles = []string{gentestrepo.RoleReference, gentestrepo.RoleFace, gentestrepo.RoleStyle, gentestrepo.RoleEdit}

// CheckRole fills the role ("" is the whole picture) and refuses a role the media has not (first / last frame are video
// only, face / style / edit image only) and a strength that is out of range, or given to a video input.
func CheckRole(media, role string, strength *float64) (string, error) {
	if role == "" {
		role = gentestrepo.RoleReference
	}
	isVideo := media == gentestrepo.MediaVideo
	switch role {
	case gentestrepo.RoleReference:
	case gentestrepo.RoleFirstFrame, gentestrepo.RoleLastFrame:
		if !isVideo {
			return role, gterr.Invalid("最初・最後のフレームは動画だけで使えます")
		}
	case gentestrepo.RoleFace, gentestrepo.RoleStyle, gentestrepo.RoleEdit:
		if isVideo {
			return role, gterr.Invalid("顔・スタイル・編集の役割は画像だけで使えます")
		}
	default:
		return role, gterr.Invalid("入力画像の役割が正しくありません: %q（reference / face / style / edit / first_frame / last_frame）", role)
	}
	if strength != nil {
		if isVideo {
			return role, gterr.Invalid("入力画像の強さは画像だけで指定できます")
		}
		if !inUnit(*strength) {
			return role, gterr.Invalid("入力画像の強さは 0〜1 にしてください")
		}
	}
	return role, nil
}

// inUnit reports whether v is within 0..1 (NaN is not).
func inUnit(v float64) bool { return v >= 0 && v <= 1 && !math.IsNaN(v) }

// CheckLoRA checks the LoRA of a request (nil: none) and returns it with its name trimmed and its strength filled. A LoRA is
// for image models only; the name must be given (at most MaxLoRANameRunes, no control characters: it is a file name of the
// loras folder, so '/' and '.' are fine) and the strength within 0..1.
func CheckLoRA(media, name string, strength *float64) (LoRA, error) {
	if media == gentestrepo.MediaVideo {
		return LoRA{}, gterr.Invalid("LoRA は画像だけで使えます")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return LoRA{}, gterr.Invalid("LoRA の名前を指定してください")
	}
	if utf8.RuneCountInString(name) > MaxLoRANameRunes {
		return LoRA{}, gterr.Invalid("LoRA の名前は %d 文字以内にしてください", MaxLoRANameRunes)
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return LoRA{}, gterr.Invalid("LoRA の名前に制御文字は使えません")
	}
	s := localcfg.DefaultLoRAStrength
	if strength != nil {
		if !inUnit(*strength) {
			return LoRA{}, gterr.Invalid("LoRA の強さは 0〜1 にしてください")
		}
		s = *strength
	}
	return LoRA{Name: name, Strength: s}, nil
}

// CheckAvoidPose refuses the option for video (there are no reference poses to avoid).
func CheckAvoidPose(media string, on bool) error {
	if on && media == gentestrepo.MediaVideo {
		return gterr.Invalid("「参照画像のポーズを真似ない」は画像だけで使えます")
	}
	return nil
}

// RoleFor is the role an image model is given for a reference of the role asked: that role when the model takes it, else the
// whole picture. roles are the roles of the model (empty: the whole picture only).
func RoleFor(roles []string, role string) string {
	for _, r := range roles {
		if r == role {
			return role
		}
	}
	return localcfg.RoleWhole
}

// InputOf is the input an image model is given for a reference: the role it takes and the strength asked (nil: the default).
func InputOf(roles []string, role string, strength *float64) (string, float64) {
	if strength == nil {
		return RoleFor(roles, role), 0
	}
	return RoleFor(roles, role), max(*strength, MinStrength)
}

// WithAvoidPose adds localcfg.AvoidPoseNegative to a negative prompt (also an empty one).
func WithAvoidPose(negative string) string {
	if negative == "" {
		return localcfg.AvoidPoseNegative
	}
	return negative + ", " + localcfg.AvoidPoseNegative
}
