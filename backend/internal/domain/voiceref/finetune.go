package voiceref

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Finetune is the voice a Character was trained to (Issue #936, spec/architecture/model-manager/local-runtime.md §16): the two weights
// GPT-SoVITS fine-tuning wrote (the GPT part and the SoVITS part), kept in the user's data below the assets directory in the directory
// "voicetrain/<ID>/" (files GPTFile and SoVITSFile). The recording of the Reference stays what it was: the synthesis still needs a short
// reference audio of the speaker, the trained weights make the voice closer to the speaker. Like the recording, the weights are shared
// by the copies of the row (the canon and the Episodes), so the voice a canon Character was trained to is the one its Episodes inherit.
type Finetune struct {
	// ID names the directory of the weights (domain.NewID: letters, digits and "-"); the synthesis server resolves it below its own
	// weights root, so nothing but this name ever reaches it.
	ID string `json:"id"`
	// GPTSizeBytes and SoVITSSizeBytes are the sizes of the two files when they were written: a file of another size is broken.
	GPTSizeBytes    int64 `json:"gpt_size_bytes"`
	SoVITSSizeBytes int64 `json:"sovits_size_bytes"`
	// Version is the GPT-SoVITS model version the weights were trained with (the pretrained base of the runtime: "v2").
	Version string `json:"version"`
	// GPTEpochs and SoVITSEpochs are the epochs the two parts were trained for; Clips and DataSeconds are the data they were trained on.
	GPTEpochs    int `json:"gpt_epochs"`
	SoVITSEpochs int `json:"sovits_epochs"`
	Clips        int `json:"clips"`
	DataSeconds  int `json:"data_seconds"`
	// TrainedAt is when the training ended.
	TrainedAt time.Time `json:"trained_at"`
}

// The names of the two weight files inside the directory of a Finetune and the directory below the assets directory they are in.
const (
	FinetuneDir    = "voicetrain"
	GPTFileName    = "gpt.ckpt"
	SoVITSFileName = "sovits.pth"
	// FinetuneVersion is the model version this mam trains (the base weights of the runtime: GPT-SoVITS v2).
	FinetuneVersion = "v2"
)

var finetuneID = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// ErrFinetune is what Finetune.Validate refuses a value with.
var ErrFinetune = errors.New("voiceref: not a trained voice")

// ValidFinetuneID reports whether id can name the directory of weights (and be sent to the synthesis server as that name).
func ValidFinetuneID(id string) bool { return finetuneID.MatchString(id) }

// Validate holds a Finetune to what the training writes: a directory name that is safe to resolve and sizes of real files.
func (f Finetune) Validate() error {
	if !ValidFinetuneID(f.ID) {
		return fmt.Errorf("%w: id %q", ErrFinetune, f.ID)
	}
	if f.GPTSizeBytes <= 0 || f.SoVITSSizeBytes <= 0 {
		return fmt.Errorf("%w: a weight file has no size", ErrFinetune)
	}
	return nil
}

// Dir is the directory of the weights, relative to the assets directory.
func (f Finetune) Dir() string { return FinetuneDir + "/" + f.ID }

// GPTPath and SoVITSPath are the two files, relative to the assets directory.
func (f Finetune) GPTPath() string { return f.Dir() + "/" + GPTFileName }

// SoVITSPath is the SoVITS weight, relative to the assets directory.
func (f Finetune) SoVITSPath() string { return f.Dir() + "/" + SoVITSFileName }
