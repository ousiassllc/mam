package facesim

import (
	"os"
	"path/filepath"
)

// Why the engine is off (Off.Reason). They are also the values of the skipped reason a result records (gentest record.FaceSimilarity.Skipped).
const (
	// ReasonModelsMissing: a model file (YuNet or SFace) is not in the model folder.
	ReasonModelsMissing = "models_missing"
	// ReasonRuntimeMissing: the models are there, but the ONNX Runtime library is missing or does not load.
	ReasonRuntimeMissing = "runtime_unavailable"
)

// Off is why the engine could not be made (NewONNX answered ErrUnavailable): what the application tells the user, the result of a generation test
// that has a face reference, and the MCP tools (#1026). It carries facts only; the words are the application's (apperr).
type Off struct {
	// Reason is ReasonModelsMissing or ReasonRuntimeMissing.
	Reason string
	// ModelDir is the folder the models are looked up in.
	ModelDir string
	// Missing are the names of the model files that are not there (ReasonModelsMissing).
	Missing []string
	// Detail is what the failure said (ReasonRuntimeMissing: the loader's message; never a secret).
	Detail string
}

// Diagnose says why NewONNX(cfg) failed with err: the model files that are missing when some are, else the runtime library.
func (c ONNXConfig) Diagnose(err error) *Off {
	off := &Off{Reason: ReasonRuntimeMissing, ModelDir: c.ModelDir}
	if err != nil {
		off.Detail = err.Error()
	}
	for _, p := range []string{c.detector(), c.recognizer()} {
		if _, statErr := os.Stat(p); statErr != nil {
			off.Reason = ReasonModelsMissing
			off.Missing = append(off.Missing, filepath.Base(p))
			if off.ModelDir == "" {
				off.ModelDir = filepath.Dir(p)
			}
		}
	}
	if off.Reason == ReasonModelsMissing {
		off.Detail = "" // the files that are missing say it
	}
	return off
}
