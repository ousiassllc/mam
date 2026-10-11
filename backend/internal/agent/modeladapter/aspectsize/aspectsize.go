// Package aspectsize maps the aspect ratio of a Brief to the image size each image model takes.
// A ratio a table lacks gets the size of domain.DefaultAspectRatio, so every domain.BriefAspectRatios
// entry must have a row (aspectsize_test.go fails when one is missing).
package aspectsize

import (
	"cmp"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

var openAI = map[string]string{
	domain.AspectRatio16x9: "1536x1024", domain.AspectRatio9x16: "1024x1536", domain.AspectRatio1x1: "1024x1024",
}

var flux = map[string][2]int{
	domain.AspectRatio16x9: {1344, 768}, domain.AspectRatio9x16: {768, 1344}, domain.AspectRatio1x1: {1024, 1024},
}

// OpenAI is the size (WxH) gpt-image takes for the aspect ratio.
func OpenAI(aspect string) string {
	return cmp.Or(openAI[aspect], openAI[domain.DefaultAspectRatio])
}

// Flux is the width and height FLUX.2 takes for the aspect ratio.
func Flux(aspect string) (width, height int) {
	size, ok := flux[aspect]
	if !ok {
		size = flux[domain.DefaultAspectRatio]
	}
	return size[0], size[1]
}
