package domain

import "slices"

// EvaluationAxis is one criterion the Judge and the Editor score (spec/spec.md §19).
type EvaluationAxis string

// Evaluation axes. Continuity and CTA belong to more than one Production Template.
const (
	AxisProductAccuracy      EvaluationAxis = "product_accuracy"
	AxisBrandGuideline       EvaluationAxis = "brand_guideline"
	AxisLogoPackageAccuracy  EvaluationAxis = "logo_package_accuracy"
	AxisKeyMessage           EvaluationAxis = "key_message"
	AxisHook                 EvaluationAxis = "hook"
	AxisProductExposure      EvaluationAxis = "product_exposure"
	AxisCTA                  EvaluationAxis = "cta"
	AxisVisualQuality        EvaluationAxis = "visual_quality"
	AxisStory                EvaluationAxis = "story"
	AxisActing               EvaluationAxis = "acting"
	AxisCharacterIdentity    EvaluationAxis = "character_identity"
	AxisContinuity           EvaluationAxis = "continuity"
	AxisDialogue             EvaluationAxis = "dialogue"
	AxisEmotion              EvaluationAxis = "emotion"
	AxisCamera               EvaluationAxis = "camera"
	AxisEditingRhythm        EvaluationAxis = "editing_rhythm"
	AxisCharacterDesign      EvaluationAxis = "character_design"
	AxisStyleConsistency     EvaluationAxis = "style_consistency"
	AxisLineShapeConsistency EvaluationAxis = "line_shape_consistency"
	AxisAnimation            EvaluationAxis = "animation"
	AxisExpression           EvaluationAxis = "expression"
	AxisWorldDesign          EvaluationAxis = "world_design"
	AxisOpeningHook          EvaluationAxis = "opening_hook"
	AxisRetention            EvaluationAxis = "retention"
	AxisTempo                EvaluationAxis = "tempo"
	AxisMessageClarity       EvaluationAxis = "message_clarity"
	AxisCaption              EvaluationAxis = "caption"
	AxisPlatformFormat       EvaluationAxis = "platform_format"
)

var axisLabels = map[EvaluationAxis]string{
	AxisProductAccuracy: "Product Accuracy", AxisBrandGuideline: "Brand Guideline", AxisLogoPackageAccuracy: "Logo / Package Accuracy",
	AxisKeyMessage: "Key Message", AxisHook: "Hook", AxisProductExposure: "Product Exposure", AxisCTA: "CTA", AxisVisualQuality: "Visual Quality",
	AxisStory: "Story", AxisActing: "Acting", AxisCharacterIdentity: "Character Identity", AxisContinuity: "Continuity",
	AxisDialogue: "Dialogue", AxisEmotion: "Emotion", AxisCamera: "Camera", AxisEditingRhythm: "Editing Rhythm",
	AxisCharacterDesign: "Character Design", AxisStyleConsistency: "Style Consistency", AxisLineShapeConsistency: "Line / Shape Consistency",
	AxisAnimation: "Animation", AxisExpression: "Expression", AxisWorldDesign: "World Design",
	AxisOpeningHook: "Opening Hook", AxisRetention: "Retention", AxisTempo: "Tempo", AxisMessageClarity: "Message Clarity",
	AxisCaption: "Caption", AxisPlatformFormat: "Platform Format",
}

// Label is the display name of the axis.
func (a EvaluationAxis) Label() string {
	if l, ok := axisLabels[a]; ok {
		return l
	}
	return string(a)
}

// templateAxes lists the axes of every Production Template from the highest to the lowest priority
// (spec/spec.md §19.1-§19.4).
var templateAxes = map[ProductionTemplate][]EvaluationAxis{
	TemplateProductCM: {
		AxisProductAccuracy, AxisBrandGuideline, AxisLogoPackageAccuracy, AxisKeyMessage,
		AxisHook, AxisProductExposure, AxisCTA, AxisVisualQuality,
	},
	TemplateDrama: {
		AxisStory, AxisActing, AxisCharacterIdentity, AxisContinuity,
		AxisDialogue, AxisEmotion, AxisCamera, AxisEditingRhythm,
	},
	TemplateAnime: {
		AxisCharacterDesign, AxisStyleConsistency, AxisLineShapeConsistency, AxisAnimation,
		AxisExpression, AxisWorldDesign, AxisContinuity,
	},
	TemplateSNSShort: {
		AxisOpeningHook, AxisRetention, AxisTempo, AxisMessageClarity,
		AxisCaption, AxisPlatformFormat, AxisCTA,
	},
}

// ProductionTemplates lists every Production Template in display order.
func ProductionTemplates() []ProductionTemplate {
	return []ProductionTemplate{TemplateProductCM, TemplateDrama, TemplateAnime, TemplateSNSShort}
}

// Valid reports whether t is one of the four Production Templates.
func (t ProductionTemplate) Valid() bool { return slices.Contains(ProductionTemplates(), t) }

// Label is the display name of the template.
func (t ProductionTemplate) Label() string {
	switch t {
	case TemplateProductCM:
		return "PRODUCT / CM"
	case TemplateDrama:
		return "DRAMA"
	case TemplateAnime:
		return "ANIME"
	case TemplateSNSShort:
		return "SNS / SHORT VIDEO"
	}
	return string(t)
}

// Axes returns the evaluation axes of the template, the highest priority first. The Judge and the
// Editor Judge evaluate along these axes and weigh them by Weights.
func (t ProductionTemplate) Axes() []EvaluationAxis { return slices.Clone(templateAxes[t]) }

// Weights returns the weight of every axis of the template: linear in the priority rank (the
// highest priority of n axes weighs n, the lowest 1) and normalised to sum to 1.
func (t ProductionTemplate) Weights() map[EvaluationAxis]float64 {
	axes := templateAxes[t]
	n := len(axes)
	total := float64(n * (n + 1) / 2)
	w := make(map[EvaluationAxis]float64, n)
	for i, a := range axes {
		w[a] = float64(n-i) / total
	}
	return w
}

// WeightedScore is the overall score of one evaluation: the mean of the scores of the template's
// axes weighted by their priority. Axes without a score are left out (the remaining weights are
// renormalised) and scores of axes that do not belong to the template are ignored. It returns false
// when no axis of the template has a score.
func (t ProductionTemplate) WeightedScore(scores map[EvaluationAxis]float64) (float64, bool) {
	var sum, weight float64
	weights := t.Weights()
	for _, a := range templateAxes[t] { // in priority order: the float sum does not depend on map order
		s, ok := scores[a]
		if !ok {
			continue
		}
		sum += s * weights[a]
		weight += weights[a]
	}
	if weight == 0 {
		return 0, false
	}
	return sum / weight, true
}
