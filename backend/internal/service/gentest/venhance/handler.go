package venhance

import "github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"

// Op is an alias, so that the handler (which may not import agent packages) can name what is done to the video (cloudenhance.Op).
type Op = cloudenhance.Op

// The operations and the provider the handler tells apart (cloudenhance's).
const (
	OpUpscale     = cloudenhance.OpUpscale
	OpInterpolate = cloudenhance.OpInterpolate
	ProviderTopaz = cloudenhance.ProviderTopaz
)
