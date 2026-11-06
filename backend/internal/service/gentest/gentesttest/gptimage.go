package gentesttest

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
)

// SolidPNG is a w × h PNG of one colour (alpha a: 0 is a fully transparent picture, 255 an opaque one): the markup layer and the image it is laid over in the
// tests of GPT Image 2.6.
func SolidPNG(w, h int, a uint8) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: 200, G: 30, B: 30, A: a})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// WithSubscriptionImageModels gives the ChatGPT-subscription twins of GPT Image 2.6 (chatgptid.Flare, Sunburst) a fake backend, so that they can be chosen
// (their requests with GPT Image 2.6's settings are refused before any call).
func WithSubscriptionImageModels() Option {
	return func(r *Rig, _ *gentest.Deps) {
		r.extra = append(r.extra, chatgptid.Flare, chatgptid.Sunburst)
		// registered process-wide (like the application's own registration) until the test ends
		localbackends.Options{ChatGPTImage: imageBackend{r.Fake, chatgptid.Flare}, ChatGPTImageEnabled: func() bool { return true }}.Publish(r.Config)
		r.T.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	}
}
