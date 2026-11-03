// Package redo puts the settings a result was made with back into the terms of the form (Issue #977): "似た感じでもう1枚" (the same settings, a
// new seed) and "この設定でもう一度" (the same settings and the same seed) open the compose form with them. An image that was uploaded is put back from the
// copy the batch kept (gentest.FromBatchInput). What cannot be put back (a model that is gone, a seed the model never took) is said, not skipped.
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

// SameEnglishNote is the note of a replay with the same seed that puts the English of the earlier run into the form as it is.
const SameEnglishNote = "同じ絵にするため、前回送った英訳をそのまま入れました（英訳はやり直しません。元の文は結果の詳細にあります）"

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
	// SpeedMode is the speed mode the model was asked for ("": its default).
	SpeedMode string
	// Notes say what could not be put back, and why.
	Notes []string
}

// Of reads the settings out of the result d. sameSeed keeps its seed (when the model took one).
func Of(d record.ResultDetail, sameSeed bool) Restored {
	r := Restored{
		Model: d.Model, Prompt: d.Prompt, Negative: d.NegativePrompt, Translate: d.TranslateToEnglish, Aspect: d.AspectRatio,
		AvoidPose: d.AvoidReferencePose, SpeedMode: d.SpeedMode,
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
		if d.Translated && d.TranslatedPrompt != "" {
			// The same seed makes the same picture only from the same words: a translation done again may come out differently. The English
			// that was sent goes into the form as it is, and the translation is off for this run.
			r.Prompt, r.Negative, r.Translate = d.TranslatedPrompt, d.TranslatedNegativePrompt, false
			r.Notes = append(r.Notes, SameEnglishNote)
		}
	default:
		r.Notes = append(r.Notes, "このモデルはシードを受け取らないため、同じ絵は再現できません（同じ設定で作り直します）")
	}
	for _, in := range d.Inputs {
		r.Refs = append(r.Refs, RefOf(in))
	}
	return r
}

// RefOf is the input image of the form that puts the input in of a result back. An image that was uploaded (or was itself put back from an earlier batch) is
// taken from the copy this batch kept (FromBatchInput); an image of the history, a Project or the pose library from where it came from.
func RefOf(in record.InputView) gentest.RefInput {
	switch in.Origin.Kind {
	case gentest.FromStaged, gentest.FromBatchInput:
		return gentest.RefInput{Source: gentest.FromBatchInput, ID: in.ID, Role: in.Role, Strength: in.Strength}
	}
	return gentest.RefInput{Source: in.Origin.Kind, ID: in.Origin.ID, ProjectID: in.Origin.ProjectID, Role: in.Role, Strength: in.Strength}
}

// SpeedGone is the note of a speed mode that the model of the form does not have (it is made in the model's default).
func SpeedGone(id string) string {
	return fmt.Sprintf("速さのモード %s はこのモデルにないため既定で作ります", id)
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
