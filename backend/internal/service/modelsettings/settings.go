// Package modelsettings reads and writes the image / video model assignment of the settings screen:
// the images (one route per purpose) and policies (one route per Quality Policy) of the Model
// Adapter setting file (<data-dir>/models.json). It sits beside package service, which is over its
// line limit, and does not import it: its refusals are ErrPinned and *InvalidError.
//
// The file holds only what differs from modeladapter.DefaultConfig (the same sparse JSON
// modeladapter.LoadConfig reads), so that a later change of a default still applies. Every other key of
// the file (vendors, models, prices, routing, retry, breaker, prefer_openai) is kept as it is. A saved
// assignment is read when the app starts: the clients are built once.
package modelsettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/settingsfile"
)

// ErrPinned means the operator passed --model-config (MAM_MODEL_CONFIG): that file is theirs and is
// never written by the settings screen.
var ErrPinned = errors.New("modelsettings: the settings file is pinned by a start-up option")

// InvalidError is a refused input or a settings file that cannot be used. Message is shown to the user.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return "invalid model settings: " + e.Message }

// Keys of the file this package owns.
const (
	keyImages   = "images"
	keyPolicies = "policies"
)

// The route types of the Model Adapter setting are aliases, so that the handler (which may not import
// agent packages) can build and read an assignment.
type (
	// ImagePurpose selects the image model (modeladapter.ImagePurpose).
	ImagePurpose = modeladapter.ImagePurpose
	// ImageRoute is the primary and the alternative image model of a purpose.
	ImageRoute = modeladapter.ImageRoute
	// ImageTarget is one image model, as its fast variant or not.
	ImageTarget = modeladapter.ImageTarget
	// PolicyRoute is the video models and the number of Takes of a Quality Policy.
	PolicyRoute = modeladapter.PolicyRoute
	// VideoTarget is one video model at one resolution.
	VideoTarget = modeladapter.VideoTarget
)

// The image purposes the screen describes by name (modeladapter's).
const (
	PurposeNewReference  = modeladapter.PurposeNewReference
	PurposeEditReference = modeladapter.PurposeEditReference
	PurposePreview       = modeladapter.PurposePreview
	PurposeUnifiedOpenAI = modeladapter.PurposeUnifiedOpenAI
)

// ImagePurposes lists the image purposes in the order the screen shows them.
func ImagePurposes() []ImagePurpose { return modeladapter.ImagePurposes() }

// Input is the whole assignment the screen submits: a route for every image purpose and every Quality Policy.
type Input struct {
	Images   map[modeladapter.ImagePurpose]modeladapter.ImageRoute
	Policies map[domain.QualityPolicy]modeladapter.PolicyRoute
}

// VideoOption is a video model and the resolutions the assignment may use with it: the resolutions the
// model supports (modeladapter.Capabilities) and that have a price in the settings.
type VideoOption struct {
	Model       domain.ModelName
	Resolutions []string
}

// ImageOption is an image model. Fast: it has a fast variant. AcceptsReferences: it uses reference
// images (modeladapter.AcceptsImageInputs); otherwise the adapter drops them.
type ImageOption struct {
	Model             domain.ModelName
	Fast              bool
	AcceptsReferences bool
}

// View is the saved assignment and what it may be set to.
type View struct {
	// Pinned: the file was fixed by --model-config; the assignment is shown but cannot be changed.
	Pinned bool
	// Customized: the file overrides at least one image purpose or Quality Policy.
	Customized  bool
	Images      map[modeladapter.ImagePurpose]modeladapter.ImageRoute
	Policies    map[domain.QualityPolicy]modeladapter.PolicyRoute
	ImageModels []ImageOption
	VideoModels []VideoOption
}

// Service edits the image / video part of one Model Adapter setting file.
type Service struct {
	path   string
	pinned bool
	// onChange, when set, runs after a change was saved (SetOnChange).
	onChange func()
}

// SetOnChange registers f to run after Save or Reset wrote the file: the app applies the saved assignment
// at once (localmodels.Service.Reload). It must be set before the service is used.
func (s *Service) SetOnChange(f func()) { s.onChange = f }

func (s *Service) changed() {
	if s.onChange != nil {
		s.onChange()
	}
}

// New returns the Service of the file at path. pinned means the operator fixed the file
// (config.Config.ModelSettingsFile): it is read, never written. An empty path has no file: the defaults
// are shown and nothing can be saved.
func New(path string, pinned bool) *Service { return &Service{path: path, pinned: pinned} }

// View returns the saved assignment: the file's, else the defaults (not the running one: a saved
// change applies after a restart). It fails when the file cannot be read as a Model Adapter setting.
func (s *Service) View() (View, error) {
	defer settingsfile.Lock(s.path)()
	cfg, err := s.load()
	if err != nil {
		return View{}, err
	}
	def := modeladapter.DefaultConfig()
	return View{
		Pinned:      s.pinned,
		Customized:  !maps.Equal(cfg.Images, def.Images) || !maps.Equal(cfg.Policies, def.Policies),
		Images:      cfg.Images,
		Policies:    cfg.Policies,
		ImageModels: imageOptions(cfg),
		VideoModels: videoOptions(cfg),
	}, nil
}

// Save stores the assignment: only the routes that differ from the defaults are written, the other keys
// of the file are kept. It writes nothing when the input or the resulting file is invalid
// (*InvalidError) and refuses a pinned file (ErrPinned). When nothing differs from the defaults the
// file is removed if no other key is left in it.
func (s *Service) Save(in Input) error {
	if s.pinned {
		return ErrPinned
	}
	defer settingsfile.Lock(s.path)()
	cfg, err := s.load()
	if err != nil {
		return err
	}
	if err := check(cfg, in); err != nil {
		return err
	}
	raw, err := s.readKeys()
	if err != nil {
		return err
	}
	def := modeladapter.DefaultConfig()
	images := map[modeladapter.ImagePurpose]imageRouteJSON{}
	for _, p := range modeladapter.ImagePurposes() {
		if in.Images[p] != def.Images[p] {
			images[p] = routeJSON(in.Images[p])
		}
	}
	policies := map[domain.QualityPolicy]modeladapter.PolicyRoute{}
	for _, q := range domain.QualityPolicies() {
		if in.Policies[q] != def.Policies[q] {
			policies[q] = in.Policies[q]
		}
	}
	if err := setKey(raw, keyImages, images); err != nil {
		return err
	}
	if err := setKey(raw, keyPolicies, policies); err != nil {
		return err
	}
	return s.committed(raw)
}

// Reset removes the image and policy overrides from the file, back to the defaults; the other keys are
// kept and a file left with none is removed. A pinned file is refused (ErrPinned).
func (s *Service) Reset() error {
	if s.pinned {
		return ErrPinned
	}
	defer settingsfile.Lock(s.path)()
	raw, err := s.readKeys()
	if err != nil {
		return err
	}
	delete(raw, keyImages)
	delete(raw, keyPolicies)
	return s.committed(raw)
}

// fileToLoad is the path to hand to configcheck.Load: a missing file of the data dir is the defaults
// (as at startup), a missing pinned file is an error there.
func (s *Service) fileToLoad() string {
	if s.path == "" {
		return ""
	}
	if _, err := os.Stat(s.path); errors.Is(err, os.ErrNotExist) && !s.pinned {
		return ""
	}
	return s.path
}

// load is the Config the file stands for, checked as at startup.
func (s *Service) load() (modeladapter.Config, error) {
	cfg, err := configcheck.Load(s.fileToLoad())
	if err != nil {
		return modeladapter.Config{}, &InvalidError{Message: "画像・動画モデルの設定ファイルを読み込めません: " + err.Error()}
	}
	return cfg, nil
}

// readKeys is the top-level keys of the file as raw JSON (none for a missing file), so that the keys this
// package does not own are written back as they were.
func (s *Service) readKeys() (settingsfile.Object, error) {
	if s.path == "" {
		return nil, errors.New("modelsettings: no settings file is configured")
	}
	raw, err := settingsfile.Read(s.path)
	if err != nil {
		return nil, &InvalidError{Message: "画像・動画モデルの設定ファイルを読み込めません: " + err.Error()}
	}
	return raw, nil
}

// commit checks the resulting file with the loader the app uses at startup, then replaces the file
// atomically; no key left removes it.
func (s *Service) commit(raw settingsfile.Object) error {
	if len(raw) > 0 {
		data, err := settingsfile.Marshal(raw)
		if err != nil {
			return err
		}
		if err := checkFile(data); err != nil {
			return err
		}
	}
	return settingsfile.Write(s.path, raw)
}

// committed is commit followed by onChange.
func (s *Service) committed(raw settingsfile.Object) error {
	if err := s.commit(raw); err != nil {
		return err
	}
	s.changed()
	return nil
}

// checkFile runs configcheck.Load (modeladapter.LoadConfig and Validate) over data, which only reads files.
func checkFile(data []byte) error {
	tmp, err := os.CreateTemp("", "mam-models-*.json")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, werr := tmp.Write(data)
	if err := errors.Join(werr, tmp.Close()); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if _, err := configcheck.Load(tmp.Name()); err != nil {
		return &InvalidError{Message: "この割り当てでは設定が不正になります: " + err.Error()}
	}
	return nil
}

func setKey[K ~string, V any](raw settingsfile.Object, key string, entries map[K]V) error {
	if len(entries) == 0 {
		delete(raw, key)
		return nil
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	raw[key] = data
	return nil
}

// imageTargetJSON names Fast explicitly: an omitted "fast" would keep the default's value, so that a
// route could not switch it off.
type imageTargetJSON struct {
	Model domain.ModelName `json:"model"`
	Fast  bool             `json:"fast"`
}

type imageRouteJSON struct {
	Primary  imageTargetJSON `json:"primary"`
	Fallback imageTargetJSON `json:"fallback"`
}

func routeJSON(r modeladapter.ImageRoute) imageRouteJSON {
	return imageRouteJSON{
		Primary:  imageTargetJSON{Model: r.Primary.Model, Fast: r.Primary.Fast},
		Fallback: imageTargetJSON{Model: r.Fallback.Model, Fast: r.Fallback.Fast},
	}
}
