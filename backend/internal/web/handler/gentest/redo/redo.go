// Package redo puts the settings a result was made with back into the terms of the form (Issue #977): "似た感じでもう1枚" (the same settings, a
// new seed) and "この設定でもう一度" (the same settings and the same seed) open the compose form with them. What cannot be put back (an image that
// was uploaded and is not kept as an input, a model that is gone, a seed the model never took) is said, not skipped.
package redo

import (
	"fmt"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// Query names of the page that opens the form with a result's settings.
const (
	// ParamAgain is the result to take the settings of; ParamSameSeed ("1") keeps its seed, otherwise the seed is left to chance.
	ParamAgain    = "again"
	ParamSameSeed = "same_seed"
)

// Restored is a result's settings in the terms of the form. A text field is "" when the result did not hold the value.
type Restored struct {
	Model              string
	Prompt, Negative   string
	Translate          bool
	Aspect             string
	Seed               string
	Steps, CFG         string
	Sampler, Scheduler string
	// Exposed are the values the model's exposed inputs ran with (by key; the form's text).
	Exposed map[string]string
	// RefStrength is the strength of a recipe's input images (nil: the recipe holds none).
	RefStrength *float64
	Refs        []gentest.RefInput
	AvoidPose   bool
	LoRA        *gentest.LoRAInput
	// Notes say what could not be put back, and why.
	Notes []string
}

// Of reads the settings out of the result d. sameSeed keeps its seed (when the model took one).
func Of(d record.ResultDetail, sameSeed bool) Restored {
	r := Restored{
		Model: d.Model, Prompt: d.Prompt, Negative: d.NegativePrompt, Translate: d.TranslateToEnglish, Aspect: d.AspectRatio,
		AvoidPose: d.AvoidReferencePose,
	}
	if d.LoRA != nil {
		s := d.LoRA.Strength
		r.LoRA = &gentest.LoRAInput{Name: d.LoRA.Name, Strength: &s}
	}
	for k, v := range d.Exposed {
		if r.Exposed == nil {
			r.Exposed = map[string]string{}
		}
		r.Exposed[k] = fmt.Sprint(v)
	}
	if s := d.Sampling; s != nil {
		if s.Steps != nil {
			r.Steps = strconv.Itoa(*s.Steps)
		}
		if s.CFG != nil {
			r.CFG = strconv.FormatFloat(*s.CFG, 'f', -1, 64)
		}
		r.Sampler, r.Scheduler = s.Sampler, s.Scheduler
	}
	switch {
	case !sameSeed:
	case d.SeedSent && d.Seed != nil:
		r.Seed = strconv.FormatInt(*d.Seed, 10)
	default:
		r.Notes = append(r.Notes, "このモデルはシードを受け取らないため、同じ絵は再現できません（同じ設定で作り直します）")
	}
	for _, in := range d.Inputs {
		if in.Origin.Kind == gentest.FromStaged {
			r.Notes = append(r.Notes, fmt.Sprintf("アップロードした入力画像（%s）は残っていないため入れていません。入れ直してください", roleName(in.Role)))
			continue
		}
		r.Refs = append(r.Refs, gentest.RefInput{Source: in.Origin.Kind, ID: in.Origin.ID, ProjectID: in.Origin.ProjectID, Role: in.Role, Strength: in.Strength})
	}
	return r
}

func roleName(role string) string {
	if role == gentest.RoleReference || role == "" {
		return "参照"
	}
	return role
}

// Missing is the note of an input image that the form can no longer offer (its source is gone).
func Missing(in gentest.RefInput) string {
	return fmt.Sprintf("入力画像（%s の %s）は見つからないため入れていません", in.Source, in.ID)
}

// Notice is the line the form opens with.
func Notice(sameSeed bool, notes []string) string {
	head := "結果の設定を入れました（シードはランダム）。見積りを確認してから実行します"
	if sameSeed {
		head = "結果の設定を、同じシードで入れました。見積りを確認してから実行します"
	}
	for _, n := range notes {
		head += "。" + n
	}
	return head
}
