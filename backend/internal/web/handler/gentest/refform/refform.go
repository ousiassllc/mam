// Package refform reads and writes the input images of the generation test form: the chosen images with their role and
// strength (ref, role_<ref>, strength_<ref>), the options that go with them (avoid_pose, lora, lora_strength, speed_mode) and the files
// the browser sent (upload, upload_first, upload_last). The handlers of web/handler/gentest use it for the compose form, the
// estimate and the hidden fields of the confirmation.
package refform

import (
	"errors"
	"math"
	"mime/multipart"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

// Value is an input image as the form names it: "<source>:<project id>:<id>".
type Value struct{ Source, ProjectID, ID string }

func (r Value) String() string { return r.Source + ":" + r.ProjectID + ":" + r.ID }

// Parse reads a value of the field ref.
func Parse(v string) (Value, bool) {
	p := strings.SplitN(v, ":", 3)
	if len(p) != 3 || p[0] == "" || p[2] == "" {
		return Value{}, false
	}
	return Value{Source: p[0], ProjectID: p[1], ID: p[2]}, true
}

// ValueOf is the value of the field ref that names the input.
func ValueOf(r gentest.RefInput) string {
	return Value{Source: r.Source, ProjectID: r.ProjectID, ID: r.ID}.String()
}

// Input is the input of a value of the field ref with the role and the strength the form holds for it.
func Input(r Value, role string, strength *float64) gentest.RefInput {
	return gentest.RefInput{Source: r.Source, ProjectID: r.ProjectID, ID: r.ID, Role: role, Strength: strength}
}

// Refs reads the chosen images in the order of the form. A video image has one of the roles of a video and no strength; an
// image has one of the roles of an image model (reference when the form names none, as when the controls are not drawn) and
// the strength of its slider (nil when it has none: the default).
func Refs(v url.Values, media string) []gentest.RefInput {
	var out []gentest.RefInput
	for _, raw := range v[view.FieldRef] {
		r, ok := Parse(raw)
		if !ok {
			continue
		}
		role := v.Get(view.FieldRolePrefix + raw)
		if media == gentest.MediaVideo {
			out = append(out, Input(r, videoRole(role), nil))
		} else {
			out = append(out, Input(r, imageRole(role), strengthOf(v.Get(view.FieldStrengthPrefix+raw))))
		}
	}
	return out
}

func videoRole(v string) string {
	if v == gentest.RoleFirstFrame || v == gentest.RoleLastFrame {
		return v
	}
	return gentest.RoleReference
}

// ImageRole is the role of an image input named by v (reference when it is none of the roles of an image model).
func ImageRole(v string) string { return imageRole(v) }

func imageRole(v string) string {
	if slices.Contains([]string{gentest.RoleFace, gentest.RoleStyle, gentest.RoleEdit, gentest.RolePose}, v) {
		return v
	}
	return gentest.RoleReference
}

// strengthOf reads a slider's value: nil when it is not a number (the default is used), else within 0..1. A number too large
// for a float (1e999) or an infinity is clamped like any other out-of-range value, not taken for "not given".
func strengthOf(v string) *float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) || math.IsNaN(f) {
		return nil
	}
	f = min(max(f, 0), 1)
	return &f
}

// Opts are the options of a batch besides its inputs: whether the pose of the references is not to be copied and the LoRA (nil
// for none) of an image batch, the speed mode of a batch ("" for each model's default).
type Opts struct {
	AvoidPose bool
	LoRA      *gentest.LoRAInput
	SpeedMode string
}

// Options reads the options of a batch.
func Options(v url.Values) Opts {
	o := Opts{AvoidPose: slices.Contains([]string{"on", "1", "true"}, v.Get(view.FieldAvoidPose)), SpeedMode: strings.TrimSpace(v.Get(view.FieldSpeedMode))}
	if name := strings.TrimSpace(v.Get(view.FieldLoRA)); name != "" {
		o.LoRA = &gentest.LoRAInput{Name: name, Strength: strengthOf(v.Get(view.FieldLoRAStrength))}
	}
	return o
}

// SpeedChoices are the speed modes the models offer, without duplicates (the first label of an ID wins).
func SpeedChoices(models []modelcatalog.Info) []view.SpeedChoice {
	var out []view.SpeedChoice
	for _, m := range models {
		for _, s := range m.SpeedModes {
			if !slices.ContainsFunc(out, func(c view.SpeedChoice) bool { return c.ID == s.ID }) {
				out = append(out, view.SpeedChoice{ID: s.ID, Label: s.Label})
			}
		}
	}
	return out
}

// Hidden carries the images and the options of a request to the confirmation, so that "実行" runs the same request.
func Hidden(req gentest.Request) []view.Hidden {
	var h []view.Hidden
	for _, r := range req.References {
		v := ValueOf(r)
		h = append(h, view.Hidden{Name: view.FieldRef, Value: v}, view.Hidden{Name: view.FieldRolePrefix + v, Value: r.Role})
		if r.Strength != nil {
			h = append(h, view.Hidden{Name: view.FieldStrengthPrefix + v, Value: strconv.FormatFloat(*r.Strength, 'f', -1, 64)})
		}
	}
	if req.AvoidReferencePose {
		h = append(h, view.Hidden{Name: view.FieldAvoidPose, Value: "on"})
	}
	if req.SpeedMode != "" {
		h = append(h, view.Hidden{Name: view.FieldSpeedMode, Value: req.SpeedMode})
	}
	if l := req.LoRA; l != nil {
		h = append(h, view.Hidden{Name: view.FieldLoRA, Value: l.Name})
		if l.Strength != nil {
			h = append(h, view.Hidden{Name: view.FieldLoRAStrength, Value: strconv.FormatFloat(*l.Strength, 'f', -1, 64)})
		}
	}
	return h
}

// Stage stores the files that came with the form and puts them into refs. The browser names an image it holds as a file by
// view.UploadPlaceholder and its number among the files of the field upload ("upload::#2" is the second): the file takes the
// place, the role and the strength of that entry. The numbers are positions among the files that arrived, so a file that is not
// taken (empty) keeps its number: the entry that names it is dropped and the entries after it still get their own files. An entry
// that names no usable file (out of range, not a number, a number already taken by an earlier entry) is dropped. A file that no
// entry names (a form without the browser's script) is added as a free reference; the first and the last frame of a video
// follow.
func Stage(refs []gentest.RefInput, form *multipart.Form, stage func(*multipart.FileHeader) (string, error)) ([]gentest.RefInput, error) {
	// staged is the id of each file of a field by position: "" for a file that is empty (not stored).
	staged := func(field string) (ids []string, err error) {
		if form == nil {
			return nil, nil
		}
		for _, fh := range form.File[field] {
			if fh.Size == 0 {
				ids = append(ids, "")
				continue
			}
			id, err := stage(fh)
			if err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, nil
	}
	uploads, err := staged(view.FieldUpload)
	if err != nil {
		return nil, err
	}
	first, err := staged(view.FieldFirst)
	if err != nil {
		return nil, err
	}
	last, err := staged(view.FieldLast)
	if err != nil {
		return nil, err
	}
	used := make([]bool, len(uploads))
	var out []gentest.RefInput
	for _, r := range refs {
		if isPlaceholder(r) {
			n, ok := placeholderNumber(r)
			if !ok || n < 1 || n > len(uploads) || uploads[n-1] == "" || used[n-1] {
				continue // its file did not arrive, or is empty, or is already in another entry
			}
			used[n-1] = true
			r.ID = uploads[n-1]
		}
		out = append(out, r)
	}
	for i, id := range uploads {
		if id != "" && !used[i] {
			out = append(out, gentest.RefInput{Source: gentest.FromStaged, ID: id, Role: gentest.RoleReference})
		}
	}
	for _, ids := range []struct {
		ids  []string
		role string
	}{{first, gentest.RoleFirstFrame}, {last, gentest.RoleLastFrame}} {
		for _, id := range ids.ids {
			if id != "" {
				out = append(out, gentest.RefInput{Source: gentest.FromStaged, ID: id, Role: ids.role})
			}
		}
	}
	return out, nil
}

// isPlaceholder says an entry of the form stands for a file of the field upload ("upload::#<n>").
func isPlaceholder(r gentest.RefInput) bool {
	return r.Source == gentest.FromStaged && strings.HasPrefix(r.ID, "#")
}

// placeholderNumber is the number of the file a placeholder stands for.
func placeholderNumber(r gentest.RefInput) (int, bool) {
	n, err := strconv.Atoi(r.ID[1:])
	return n, err == nil
}
