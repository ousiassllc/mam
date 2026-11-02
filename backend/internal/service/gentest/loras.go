package gentest

import "context"

// LoRAs lists the LoRA files of the bundled ComfyUI engine, sorted: the names a request's LoRA may take
// (modelcatalog.Info.SupportsLoRA says which models use it). Empty when the engine is not running or has none, and always
// for media video: a LoRA is for images only, so the engine is not asked ("" is both media).
func (s *Service) LoRAs(ctx context.Context, media string) []string {
	if media == MediaVideo {
		return nil
	}
	ep, ok := s.Adapter.Config().Endpoints[s.BundledEndpoint]
	if !ok {
		return nil
	}
	return s.loras.List(ctx, s.BundledEndpoint, ep)
}
