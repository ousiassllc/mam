package localmodels

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential"
)

// EndpointInput is an endpoint to create or update. Name and Kind identify the endpoint for good: models and
// the identifiers in take.model_name refer to them, so an update may only repeat them (or leave them empty).
type EndpointInput struct {
	Name string
	Kind localcfg.Kind
	// BaseURL is the server's address: http://127.0.0.1:8188, a LAN address, or a tailnet address.
	BaseURL string
	// MaxConcurrency (0: 1) and TimeoutSeconds (0: 3600 for ComfyUI, 900 for OpenAI-compatible) default as in
	// localcfg.Endpoint.
	MaxConcurrency int
	TimeoutSeconds int
}

// reservedEndpointName is the last segment of the address of the form that adds an endpoint
// (/settings/local-models/endpoints/new), so an endpoint of that name could not have a page of its own.
const reservedEndpointName = "new"

func (in EndpointInput) endpoint() localcfg.Endpoint {
	return localcfg.Endpoint{Kind: in.Kind, BaseURL: strings.TrimSpace(in.BaseURL), MaxConcurrency: in.MaxConcurrency, TimeoutSeconds: in.TimeoutSeconds}
}

// CreateEndpoint registers an endpoint. A name in use is ErrExists; a bad input is *InvalidError.
func (s *Service) CreateEndpoint(_ context.Context, in EndpointInput) error {
	return s.change(func(st *state, _ modeladapter.Config) error {
		if in.Name == reservedEndpointName {
			return invalid("「%s」は接続先の名前に使えません（接続先の追加の画面の URL と同じなため）。別の名前にしてください", reservedEndpointName)
		}
		if _, ok := st.endpoints[in.Name]; ok {
			return ErrExists
		}
		if err := firstError(localcfg.ValidateEndpoint(in.Name, in.endpoint())); err != nil {
			return err
		}
		st.endpoints[in.Name] = in.endpoint()
		return nil
	})
}

// UpdateEndpoint changes the address, concurrency and timeout of the endpoint called name. A different Name
// or Kind in the input is refused: a rename is a delete and a create (the old identifiers stay in the
// history of the Takes and the Cost Ledger).
func (s *Service) UpdateEndpoint(_ context.Context, name string, in EndpointInput) error {
	return s.change(func(st *state, _ modeladapter.Config) error {
		old, ok := st.endpoints[name]
		switch {
		case !ok:
			return ErrNotFound
		case in.Name != "" && in.Name != name:
			return invalid("接続先の名前は変更できません。削除して作り直してください（Take とコスト台帳に残る識別子が名前でできているため）")
		case in.Kind != "" && in.Kind != old.Kind:
			return invalid("接続先の種類は変更できません。削除して作り直してください")
		}
		in.Name, in.Kind = name, old.Kind
		if err := firstError(localcfg.ValidateEndpoint(name, in.endpoint())); err != nil {
			return err
		}
		st.endpoints[name] = in.endpoint()
		return nil
	})
}

// DeleteEndpoint removes the endpoint, the models defined on it and its API key. An endpoint whose model
// is chosen in an image purpose or a Quality Policy is refused (*InvalidError): change the assignment first.
func (s *Service) DeleteEndpoint(_ context.Context, name string) error {
	err := s.change(func(st *state, cfg modeladapter.Config) error {
		ep, ok := st.endpoints[name]
		if !ok {
			return ErrNotFound
		}
		for key := range st.models {
			if e, n, _ := localcfg.SplitKey(key); e == name {
				if routes := usedBy(cfg, localcfg.ID(ep.Kind, e, n)); len(routes) > 0 {
					return invalid("モデル %s は %s で使われています。先に割り当てを変えてください", key, strings.Join(routes, "、"))
				}
				delete(st.models, key)
			}
		}
		delete(st.endpoints, name)
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.Keys.Delete(localcfg.VendorKey(name)); err != nil && !errors.Is(err, credential.ErrNotFound) {
		return err
	}
	return nil
}

// KeyStatus is whether an endpoint has an API key. It never carries the key: only whether one is stored and
// its last characters (credential.Hint).
type KeyStatus struct {
	Configured bool
	LastChars  string
	// Unavailable: the OS key store could not be read (no Secret Service, locked, ...). View reports it
	// instead of failing, so that a broken key store does not hide the endpoints; Configured is then unknown.
	Unavailable bool
}

// EndpointKeyStatus reports the API key of the endpoint called name.
func (s *Service) EndpointKeyStatus(name string) (KeyStatus, error) {
	cfg, err := s.load()
	if err != nil {
		return KeyStatus{}, err
	}
	if _, ok := cfg.Endpoints[name]; !ok {
		return KeyStatus{}, ErrNotFound
	}
	return s.keyStatus(name)
}

func (s *Service) keyStatus(name string) (KeyStatus, error) {
	vendor := localcfg.VendorKey(name)
	ok, err := s.Keys.Has(vendor)
	if err != nil || !ok {
		return KeyStatus{}, err
	}
	hint, err := credential.Hint(s.Keys, vendor)
	return KeyStatus{Configured: true, LastChars: hint}, err
}

// SetEndpointKey stores or replaces the API key of the endpoint called name (optional for an endpoint;
// the adapters read it on every call, so it is in effect at once). It is allowed for a pinned file too: the
// key is not in the file. An empty key, one with whitespace inside or one too long is *InvalidError.
func (s *Service) SetEndpointKey(name, apiKey string) error {
	cfg, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := cfg.Endpoints[name]; !ok {
		return ErrNotFound
	}
	apiKey, problem := credential.CleanAPIKey(apiKey)
	if problem != "" {
		return invalid("%s", problem)
	}
	if err := s.Keys.Set(localcfg.VendorKey(name), apiKey); errors.Is(err, credential.ErrTooLarge) {
		return invalid("API キーが長すぎます")
	} else if err != nil {
		return err
	}
	return nil
}

// ClearEndpointKey removes the API key of the endpoint called name; removing a missing key is not an error.
func (s *Service) ClearEndpointKey(name string) error {
	cfg, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := cfg.Endpoints[name]; !ok {
		return ErrNotFound
	}
	if err := s.Keys.Delete(localcfg.VendorKey(name)); err != nil && !errors.Is(err, credential.ErrNotFound) {
		return err
	}
	return nil
}

// usedBy lists where the model is chosen: image purposes (primary / fallback), Quality Policies and the
// Multi-model routing candidates.
func usedBy(cfg modeladapter.Config, id string) []string {
	var used []string
	for _, p := range slices.Sorted(maps.Keys(cfg.Images)) {
		r := cfg.Images[p]
		if string(r.Primary.Model) == id {
			used = append(used, "images."+string(p)+".primary")
		}
		if string(r.Fallback.Model) == id {
			used = append(used, "images."+string(p)+".fallback")
		}
	}
	for _, q := range slices.Sorted(maps.Keys(cfg.Policies)) {
		r := cfg.Policies[q]
		if string(r.Primary.Model) == id {
			used = append(used, "policies."+string(q)+".primary")
		}
		if string(r.Fallback.Model) == id {
			used = append(used, "policies."+string(q)+".fallback")
		}
	}
	for _, q := range slices.Sorted(maps.Keys(cfg.Routing.Candidates)) {
		for _, t := range cfg.Routing.Candidates[q] {
			if string(t.Model) == id {
				used = append(used, "routing.candidates."+string(q))
				break
			}
		}
	}
	return used
}

// firstError is the first of errs as an *InvalidError, nil for none.
func firstError(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	err := errors.Join(errs...)
	if strings.Contains(err.Error(), "link-local") {
		return invalid("%v %s", err, linkLocalHint)
	}
	return invalid("%v", err)
}

// linkLocalHint says in Japanese why such an address is refused: the validation's own text is English only.
// It is never allowed, not even for an endpoint the user registers.
const linkLocalHint = "（リンクローカル（169.254.x.x・fe80::）、未指定、マルチキャストのアドレスは、登録した接続先でも使えません。クラウドのメタデータサービスなどに届いてしまうためです）"
