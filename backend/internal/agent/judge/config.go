package judge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Config is the AI Video Judge setting. DefaultConfig holds the initial values; a JSON file
// (LoadConfig, --judge-config) overrides any part of it. The Face Identity Similarity thresholds are
// configuration, never code: they are measured, not derived (spec/spec.md §12.4).
type Config struct {
	Frames FrameConfig `json:"frames"`
	Face   FaceConfig  `json:"face"`
}

// FrameConfig is how much of a Take is looked at.
type FrameConfig struct {
	// FaceSampleFPS is how many frames per second the face measurement samples.
	FaceSampleFPS float64 `json:"face_sample_fps"`
	// MaxFaceFrames caps the frames of the face measurement; a long Take is sampled more sparsely.
	MaxFaceFrames int `json:"max_face_frames"`
	// FaceMaxWidth is the widest frame the face models get (wider frames are scaled down).
	FaceMaxWidth int `json:"face_max_width"`
	// ReviewFrames is how many evenly spaced frames the qualitative evaluation (the Reasoning LLM) is shown.
	ReviewFrames int `json:"review_frames"`
	// ReviewMaxWidth is the widest frame the Reasoning LLM gets.
	ReviewMaxWidth int `json:"review_max_width"`
	// MaxReferenceImages caps the Reference Pack images shown to the Reasoning LLM next to the frames.
	MaxReferenceImages int `json:"max_reference_images"`
}

// FaceConfig is the Face Identity Similarity setting. Similarities are cosine similarities
// between the SFace embedding of a Take's face and of the Character's reference faces.
type FaceConfig struct {
	// WarnSimilarity and FailSimilarity are the two thresholds used when a Character's thresholds
	// cannot be calibrated (fewer than two usable reference images, or references that do not
	// resemble each other). A frame below WarnSimilarity is a warning, below FailSimilarity a failure.
	WarnSimilarity float64 `json:"warn_similarity"`
	FailSimilarity float64 `json:"fail_similarity"`
	// CalibrationWarnScale and CalibrationFailScale calibrate the thresholds on the Reference Pack:
	// threshold = scale × the mean pairwise similarity of the Character's reference images.
	CalibrationWarnScale float64 `json:"calibration_warn_scale"`
	CalibrationFailScale float64 `json:"calibration_fail_scale"`
	// MinCalibrationSimilarity is the mean pairwise similarity the references must reach for the
	// calibration to be trusted; below it the fixed thresholds are used.
	MinCalibrationSimilarity float64 `json:"min_calibration_similarity"`
	// MinRunFrames is how many consecutive sampled frames below a threshold make a face_identity_drift Issue.
	MinRunFrames int `json:"min_run_frames"`
	// MaxUndetectedRatio is the share of frames without a detectable face above which the numbers
	// are not evaluated and the Take is judged qualitatively only.
	MaxUndetectedRatio float64 `json:"max_undetected_ratio"`
	// MinDetectedFrames is the fewest frames with a face the numbers need.
	MinDetectedFrames int `json:"min_detected_frames"`
}

// DefaultConfig is the initial setting. The similarity values are starting points (SFace's
// published verification threshold is 0.363), to be replaced by measurement on real Takes.
func DefaultConfig() Config {
	return Config{
		Frames: FrameConfig{
			FaceSampleFPS: 4, MaxFaceFrames: 48, FaceMaxWidth: 1280,
			ReviewFrames: 12, ReviewMaxWidth: 768, MaxReferenceImages: 4,
		},
		Face: FaceConfig{
			WarnSimilarity: 0.5, FailSimilarity: 0.363,
			CalibrationWarnScale: 0.85, CalibrationFailScale: 0.65, MinCalibrationSimilarity: 0.4,
			MinRunFrames: 2, MaxUndetectedRatio: 0.5, MinDetectedFrames: 3,
		},
	}
}

// LoadConfig returns DefaultConfig overridden by the JSON file at path ("" selects the default).
// Unknown keys are an error.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the operator chooses the settings file
	if err != nil {
		return Config{}, fmt.Errorf("read judge config: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode judge config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("judge config %s: %w", path, err)
	}
	return cfg, nil
}

// Validate checks that the numbers are usable.
func (c Config) Validate() error {
	var errs []error
	f, s := c.Frames, c.Face
	if f.FaceSampleFPS <= 0 || f.MaxFaceFrames <= 0 || f.FaceMaxWidth <= 0 || f.ReviewFrames <= 0 || f.ReviewMaxWidth <= 0 || f.MaxReferenceImages < 0 {
		errs = append(errs, errors.New("frames: every value must be positive (max_reference_images may be 0)"))
	}
	if !(0 < s.FailSimilarity && s.FailSimilarity < s.WarnSimilarity && s.WarnSimilarity <= 1) {
		errs = append(errs, errors.New("face: need 0 < fail_similarity < warn_similarity <= 1"))
	}
	if !(0 < s.CalibrationFailScale && s.CalibrationFailScale < s.CalibrationWarnScale && s.CalibrationWarnScale <= 1) {
		errs = append(errs, errors.New("face: need 0 < calibration_fail_scale < calibration_warn_scale <= 1"))
	}
	if s.MinCalibrationSimilarity < 0 || s.MinCalibrationSimilarity > 1 {
		errs = append(errs, errors.New("face: min_calibration_similarity must be in 0..1"))
	}
	if s.MinRunFrames <= 0 || s.MinDetectedFrames <= 0 {
		errs = append(errs, errors.New("face: min_run_frames and min_detected_frames must be positive"))
	}
	if s.MaxUndetectedRatio < 0 || s.MaxUndetectedRatio >= 1 {
		errs = append(errs, errors.New("face: max_undetected_ratio must be in 0..1 (exclusive of 1)"))
	}
	return errors.Join(errs...)
}
