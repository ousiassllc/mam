package imagespec_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The GPT Image 2.6 options (complexity, GIF / SVG output, markup) reach OpenAI's Image API under the field names of its reference, and only for the two
// models that take them (#1096; spec/architecture/model-adapters.md §9.5).

// recorded is what an Image API stub saw of one call.
type recorded struct {
	path   string
	json   map[string]any
	fields map[string]string
	files  map[string][]byte
	names  map[string]string
}

// stubBody, when set, is what the stub answers whatever format was asked for (a vendor that does not honour the format).
var stubBody []byte

func optionsStub(t *testing.T, calls *int) (*httptest.Server, *recorded) {
	t.Helper()
	got := &recorded{fields: map[string]string{}, files: map[string][]byte{}, names: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		got.path = r.URL.Path
		if mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err == nil && mt == "multipart/form-data" {
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := mr.NextPart()
				if err == io.EOF {
					break
				}
				if !assert.NoError(t, err) {
					return
				}
				data, _ := io.ReadAll(part)
				if part.FileName() != "" {
					got.files[part.FormName()], got.names[part.FormName()] = data, part.FileName()
				} else {
					got.fields[part.FormName()] = string(data)
				}
			}
		} else {
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&got.json))
		}
		out := []byte("OUT")
		switch got.json["output_format"] {
		case "gif":
			out = []byte("GIF89a-frames")
		case "svg":
			out = []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)
		}
		if got.fields["output_format"] == "gif" {
			out = []byte("GIF89a-frames")
		}
		if stubBody != nil {
			out = stubBody
		}
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(out) + `"}],"usage":{"input_tokens":10,"output_tokens":1000,"input_tokens_details":{"text_tokens":10,"image_tokens":0}}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func optionsAdapter(t *testing.T, srv *httptest.Server) *modeladapter.Adapter {
	t.Helper()
	cfg := modeladapter.DefaultConfig()
	for v, vc := range cfg.Vendors {
		vc.BaseURL = srv.URL
		cfg.Vendors[v] = vc
	}
	return modeladapter.New(cfg, modeladapter.NewBackends(cfg, memKeys{"openai": "sk-test"}, srv.Client(), 0, modeladapter.WithLocalDownloads()))
}

func TestGPTImage26ComplexityAndGIFFieldsAreSentUnderTheirAPINames(t *testing.T) {
	for _, e := range gpt26 {
		t.Run(string(e.model)+" generation", func(t *testing.T) {
			var calls int
			srv, got := optionsStub(t, &calls)

			out, err := direct.Image(context.Background(), optionsAdapter(t, srv), e.model, modeladapter.ImageRequest{
				Prompt: "a fox", AspectRatio: "1:1",
				GPT: gptimage.Options{Complexity: "low", Format: gptimage.FormatGIF, FrameCount: 12, FrameDurationMS: 100},
			})

			require.NoError(t, err)
			assert.Equal(t, "/v1/images/generations", got.path)
			assert.Equal(t, map[string]any{
				"model": e.apiID, "prompt": "a fox", "size": "1024x1024", "quality": "medium", "n": float64(1),
				"complexity": "low", "output_format": "gif", "frame_count": float64(12), "frame_duration_ms": float64(100),
			}, got.json)
			assert.Equal(t, "image/gif", out.MimeType, "the stored type follows the format that was asked for")
			require.NotNil(t, out.ActualCost, "a GIF is paid at the image token prices of the usage")
		})
	}
}

func TestGPTImage26EditSendsComplexityAndTheMarkupLayerAsAFilePart(t *testing.T) {
	var calls int
	srv, got := optionsStub(t, &calls)

	out, err := direct.Image(context.Background(), optionsAdapter(t, srv), domain.ModelGPTImage26Flare, modeladapter.ImageRequest{
		Prompt: "recolor the vase", AspectRatio: "16:9",
		Inputs: []modeladapter.Input{{Data: []byte("KITCHEN"), MimeType: "image/webp"}, {Data: []byte("REF"), MimeType: "image/png"}},
		Markup: &modeladapter.Input{Data: []byte("CIRCLE"), MimeType: "image/png"},
		GPT:    gptimage.Options{Complexity: "high"},
	})

	require.NoError(t, err)
	assert.Equal(t, "/v1/images/edits", got.path)
	assert.Equal(t, map[string]string{"model": "gpt-image-2.6-flare", "prompt": "recolor the vase", "size": "1536x1024", "quality": "medium", "complexity": "high"}, got.fields)
	assert.Equal(t, []byte("CIRCLE"), got.files["markup"], "a `markup` file part, as in OpenAI's example")
	assert.Equal(t, "markup.png", got.names["markup"])
	assert.Equal(t, []byte("REF"), got.files["image[]"], "the references stay image[] parts")
	assert.Equal(t, "image/png", out.MimeType)
}

func TestGPTImage26SVGIsAGenerationOnlyAndItsCostIsUnknown(t *testing.T) {
	var calls int
	srv, got := optionsStub(t, &calls)
	a := optionsAdapter(t, srv)

	out, err := direct.Image(context.Background(), a, domain.ModelGPTImage26Sunburst, modeladapter.ImageRequest{
		Prompt: "a bakery logo", GPT: gptimage.Options{Format: gptimage.FormatSVG, Complexity: "medium"},
	})
	require.NoError(t, err)
	assert.Equal(t, "svg", got.json["output_format"])
	assert.Equal(t, float64(1), got.json["n"], "SVG output needs n = 1")
	assert.Equal(t, "image/svg+xml", out.MimeType)
	assert.Nil(t, out.ActualCost, "the SVG authoring model's tokens are not priced here: unknown, never the reference image's part alone")

	calls = 0
	_, err = direct.Image(context.Background(), a, domain.ModelGPTImage26Sunburst, modeladapter.ImageRequest{
		Prompt: "edit", Inputs: []modeladapter.Input{{Data: []byte("X"), MimeType: "image/png"}}, GPT: gptimage.Options{Format: gptimage.FormatSVG},
	})
	require.Error(t, err, "the edits endpoint has no SVG output")
	assert.Contains(t, err.Error(), "SVG")
	assert.Zero(t, calls, "nothing is sent")
}

func TestGPTImage26OptionsAreRefusedForEveryOtherModelAndNeverDropped(t *testing.T) {
	cases := map[string]struct {
		model  domain.ModelName
		reason string
	}{
		"gpt-image-2": {domain.ModelGPTImage2, "gpt-image-2"},
		"nano-banana": {domain.ModelNanoBanana, "nano-banana"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var calls int
			srv, _ := optionsStub(t, &calls)
			a := optionsAdapter(t, srv)

			_, err := direct.Image(context.Background(), a, tc.model, modeladapter.ImageRequest{Prompt: "p", GPT: gptimage.Options{Complexity: "low"}})
			require.ErrorIs(t, err, direct.ErrOptionsUnsupported)
			assert.Contains(t, err.Error(), tc.reason)

			_, err = direct.Image(context.Background(), a, tc.model, modeladapter.ImageRequest{
				Prompt: "p", Inputs: []modeladapter.Input{{Data: []byte("A"), MimeType: "image/png"}}, Markup: &modeladapter.Input{Data: []byte("M"), MimeType: "image/png"},
			})
			require.ErrorIs(t, err, direct.ErrOptionsUnsupported)
			assert.Zero(t, calls, "no vendor call, no billing")
		})
	}
}

// A markup layer is an edit field: with no input image it is refused before anything is sent.
func TestGPTImage26MarkupNeedsAnInputImage(t *testing.T) {
	var calls int
	srv, _ := optionsStub(t, &calls)

	_, err := direct.Image(context.Background(), optionsAdapter(t, srv), domain.ModelGPTImage26Flare, modeladapter.ImageRequest{
		Prompt: "p", Markup: &modeladapter.Input{Data: []byte("M"), MimeType: "image/png"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "マークアップ")
	assert.Zero(t, calls)
}

func TestGPTImage26QualitiesIncludeXHighAndMaxButGPTImage2DoesNot(t *testing.T) {
	assert.True(t, gptimage.ValidFor(domain.ModelGPTImage26Flare, "xhigh"))
	assert.True(t, gptimage.ValidFor(domain.ModelGPTImage26Sunburst, "max"))
	assert.False(t, gptimage.ValidFor(domain.ModelGPTImage2, "xhigh"), "OpenAI lists low / medium / high for gpt-image-2")
	assert.False(t, gptimage.ValidFor(domain.ModelGPTImage26Flare, "auto"))

	var calls int
	srv, got := optionsStub(t, &calls)
	cfg := modeladapter.DefaultConfig()
	mc := cfg.Models[domain.ModelGPTImage26Flare]
	mc.Quality = "xhigh"
	cfg.Models[domain.ModelGPTImage26Flare] = mc
	for v, vc := range cfg.Vendors {
		vc.BaseURL = srv.URL
		cfg.Vendors[v] = vc
	}
	a := modeladapter.New(cfg, modeladapter.NewBackends(cfg, memKeys{"openai": "sk-test"}, srv.Client(), 0, modeladapter.WithLocalDownloads()))

	_, err := direct.Image(context.Background(), a, domain.ModelGPTImage26Flare, modeladapter.ImageRequest{Prompt: "p"})

	require.NoError(t, err)
	assert.Equal(t, "xhigh", got.json["quality"], "the configured profile is sent as it is")
}

// An answer that is not in the format that was asked for is an error, never stored as a GIF or an SVG.
func TestGPTImage26AnAnswerInAnotherFormatIsAnError(t *testing.T) {
	for format, body := range map[string]string{gptimage.FormatGIF: "\x89PNG-not-a-gif", gptimage.FormatSVG: "\x89PNG-not-an-svg"} {
		t.Run(format, func(t *testing.T) {
			stubBody = []byte(body)
			t.Cleanup(func() { stubBody = nil })
			var calls int
			srv, _ := optionsStub(t, &calls)

			_, err := direct.Image(context.Background(), optionsAdapter(t, srv), domain.ModelGPTImage26Flare, modeladapter.ImageRequest{
				Prompt: "p", GPT: gptimage.Options{Format: format},
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), "ではありませんでした")
		})
	}
}
