package localmodels

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/settingsfile"
)

// The keys of the file this package owns.
const (
	keyEndpoints   = "endpoints"
	keyLocalModels = "local_models"
)

// fileToLoad is the path to hand to configcheck.Load: a missing file of the data directory is the defaults
// (as at start-up), a missing pinned file is an error there.
func (s *Service) fileToLoad() string {
	if s.Path == "" {
		return ""
	}
	if _, err := os.Stat(s.Path); errors.Is(err, fs.ErrNotExist) && !s.Pinned {
		return ""
	}
	return s.Path
}

// load is the Config the file stands for, checked as at start-up.
func (s *Service) load() (modeladapter.Config, error) {
	cfg, err := configcheck.Load(s.fileToLoad())
	if err != nil {
		return modeladapter.Config{}, invalid("画像・動画モデルの設定ファイルを読み込めません: %v", err)
	}
	return cfg, nil
}

// Reload reads the file again and swaps its endpoints, models and routes into the Model Adapter. It takes no
// lock (the file is replaced atomically), so that the callback of modelsettings, which holds the file's lock
// while it runs, can call it.
func (s *Service) Reload() error {
	cfg, err := s.load()
	if err != nil {
		return err
	}
	s.Adapter.Reload(cfg, localbackends.Build(cfg, s.Keys, s.Backends))
	s.Backends.Publish(cfg)
	return nil
}

// state is the endpoints and models of the file as typed maps, with the raw object they came from.
type state struct {
	raw       settingsfile.Object
	endpoints map[string]localcfg.Endpoint
	models    map[string]localcfg.Model
}

// read decodes the keys this package owns. Unknown fields are refused, as the loader does.
func (s *Service) read() (*state, error) {
	if s.Path == "" {
		return nil, errors.New("localmodels: no settings file is configured")
	}
	raw, err := settingsfile.Read(s.Path)
	if err != nil {
		return nil, invalid("画像・動画モデルの設定ファイルを読み込めません: %v", err)
	}
	st := &state{raw: raw, endpoints: map[string]localcfg.Endpoint{}, models: map[string]localcfg.Model{}}
	for key, into := range map[string]any{keyEndpoints: &st.endpoints, keyLocalModels: &st.models} {
		if data, ok := raw[key]; ok {
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.DisallowUnknownFields()
			if err := dec.Decode(into); err != nil {
				return nil, invalid("設定ファイルの %s を読み込めません: %v", key, err)
			}
		}
	}
	return st, nil
}

// change runs edit on the saved endpoints and models and saves the result: the whole file is checked as at
// start-up first (a failure writes nothing and says why), then replaced atomically, then applied at once. A
// pinned file is refused. edit returns a refusal for the user, or nil.
func (s *Service) change(edit func(st *state, cfg modeladapter.Config) error) error {
	if s.Pinned {
		return ErrPinned
	}
	defer settingsfile.Lock(s.Path)()
	cfg, err := s.load()
	if err != nil {
		return err
	}
	st, err := s.read()
	if err != nil {
		return err
	}
	if err := edit(st, cfg); err != nil {
		return err
	}
	for key, v := range map[string]any{keyEndpoints: st.endpoints, keyLocalModels: st.models} {
		if err := setKey(st.raw, key, v, mapLen(v)); err != nil {
			return err
		}
	}
	data, err := settingsfile.Marshal(st.raw)
	if err != nil {
		return err
	}
	if err := checkFile(data); err != nil {
		return err
	}
	if err := settingsfile.Write(s.Path, st.raw); err != nil {
		return err
	}
	return s.Reload()
}

func mapLen(v any) int {
	switch m := v.(type) {
	case map[string]localcfg.Endpoint:
		return len(m)
	case map[string]localcfg.Model:
		return len(m)
	}
	return 0
}

func setKey(raw settingsfile.Object, key string, v any, n int) error {
	if n == 0 {
		delete(raw, key)
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	raw[key] = data
	return nil
}

// checkFile runs configcheck.Load over data, which only reads files.
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
		return invalid("この内容では設定が不正になります: %v", err)
	}
	return nil
}
