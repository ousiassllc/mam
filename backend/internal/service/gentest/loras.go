package gentest

import "context"

// LoRAs lists the LoRA files of the bundled ComfyUI engine, sorted: the names a request's LoRA may take
// (modelcatalog.Info.SupportsLoRA says which models use it). Empty when the engine is not running or has none.
func (s *Service) LoRAs(ctx context.Context) []string {
	ep, ok := s.Adapter.Config().Endpoints[s.BundledEndpoint]
	if !ok {
		return nil
	}
	return s.loras.List(ctx, s.BundledEndpoint, ep)
}
