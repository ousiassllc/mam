package llmlocal

import (
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service/settingsfile"
)

// edit is what a save changes in providers.local of the settings file: an empty baseURL and a nil switch keep what is saved.
type edit struct {
	baseURL                       string
	unload, useManaged, useOllama *bool
}

// write sets providers.local of the settings file.
func (s *Service) write(e edit) error {
	defer settingsfile.Lock(s.path)()
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, err := settingsfile.Read(s.path)
	if err != nil {
		return &InvalidError{Message: "設定ファイル " + s.path + " を読み込めません。直すか削除してからやり直してください（" + err.Error() + "）"}
	}
	providers := map[string]json.RawMessage{}
	if raw, ok := obj["providers"]; ok {
		if err := json.Unmarshal(raw, &providers); err != nil || providers == nil {
			return &InvalidError{Message: "設定ファイル " + s.path + " の providers が読み込めません。直すか削除してからやり直してください"}
		}
	}
	local := map[string]json.RawMessage{}
	if raw, ok := providers[provider]; ok {
		if err := json.Unmarshal(raw, &local); err != nil || local == nil {
			return &InvalidError{Message: "設定ファイル " + s.path + " の providers.local が読み込めません。直すか削除してからやり直してください"}
		}
	}
	if e.baseURL != "" {
		if err := setKey(local, "base_url", e.baseURL, e.baseURL != llm.LocalDefaultBaseURL); err != nil {
			return err
		}
	}
	for key, on := range map[string]*bool{"unload_before_generation": e.unload, "use_managed_runtime": e.useManaged, "use_managed_ollama": e.useOllama} {
		if on == nil {
			continue
		}
		if err := setKey(local, key, *on, *on); err != nil {
			return err
		}
	}
	if _, ok := local["base_url"]; !ok && len(local) > 0 {
		// llm.Config reads providers.local as a whole: an entry with another key and no base_url would have an empty one.
		if err := setKey(local, "base_url", llm.LocalDefaultBaseURL, true); err != nil {
			return err
		}
	}
	if err := put(providers, provider, local); err != nil {
		return err
	}
	if err := put(obj, "providers", providers); err != nil {
		return err
	}
	data, err := settingsfile.Marshal(obj)
	if err != nil {
		return err
	}
	cfg, err := llm.ParseConfig(data)
	if err != nil {
		return &InvalidError{Message: "この設定は保存できません: " + err.Error()}
	}
	if err := settingsfile.Write(s.path, obj); err != nil {
		return err
	}
	if s.apply != nil {
		s.apply(cfg)
	}
	return nil
}

// setKey sets the key of the object to value when keep, else drops it.
func setKey(obj map[string]json.RawMessage, key string, value any, keep bool) error {
	if !keep {
		delete(obj, key)
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", key, err)
	}
	obj[key] = raw
	return nil
}

// put stores the object under key, or drops the key when the object is empty (an object with nothing to say is the default).
func put[M ~map[string]json.RawMessage](parent M, key string, obj map[string]json.RawMessage) error {
	if len(obj) == 0 {
		delete(parent, key)
		return nil
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("encode %s: %w", key, err)
	}
	parent[key] = raw
	return nil
}
