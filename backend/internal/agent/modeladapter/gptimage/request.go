package gptimage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/textproto"
)

// Image is an image that goes into a request: a reference (an input image of an edit) or the markup layer.
type Image struct {
	Data     []byte
	MimeType string
}

// Request is one call of OpenAI's Image API as it is sent.
type Request struct {
	Path        string // "/v1/images/generations" or "/v1/images/edits"
	ContentType string
	Body        []byte
}

// BuildRequest makes the request of a GPT Image model (the shapes of the guide https://developers.openai.com/api/docs/guides/image-generation and the API
// reference's Create image / Create image edit): without input images it is a generation (JSON), with them an edit (multipart, one `image[]` part each).
// The options and the markup layer (a `markup` file part laid over the first image) are sent only when set, so a request without them is exactly the one
// of GPT Image 2. It refuses what the Image API cannot do (Options.CheckRequest) instead of sending it.
func BuildRequest(apiModel string, quality Quality, size, prompt string, inputs []Image, markup *Image, o Options) (Request, error) {
	if !o.Zero() || markup != nil {
		if !TakesOptionsAPI(apiModel) {
			return Request{}, fmt.Errorf("%s は複雑さ・GIF・SVG・マークアップの指定に対応しません（GPT Image 2.6 だけ）", apiModel)
		}
		if err := o.CheckRequest(len(inputs) > 0, markup != nil); err != nil {
			return Request{}, err
		}
	}
	if len(inputs) == 0 {
		body := map[string]any{"model": apiModel, "prompt": prompt, "size": size, "quality": quality, "n": 1}
		o.JSONFields(body)
		raw, err := json.Marshal(body)
		return Request{Path: "/v1/images/generations", ContentType: "application/json", Body: raw}, err
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range append([][2]string{{"model", apiModel}, {"prompt", prompt}, {"size", size}, {"quality", string(quality)}}, o.FormFields()...) {
		_ = w.WriteField(f[0], f[1])
	}
	for i, in := range inputs {
		if err := filePart(w, "image[]", fmt.Sprintf("input%d%s", i, ext(in.MimeType)), in); err != nil {
			return Request{}, err
		}
	}
	if markup != nil {
		if err := filePart(w, "markup", "markup.png", *markup); err != nil {
			return Request{}, err
		}
	}
	if err := w.Close(); err != nil {
		return Request{}, err
	}
	return Request{Path: "/v1/images/edits", ContentType: w.FormDataContentType(), Body: buf.Bytes()}, nil
}

func filePart(w *multipart.Writer, field, name string, in Image) error {
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, name))
	h.Set("Content-Type", in.MimeType)
	part, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	_, err = part.Write(in.Data)
	return err
}

func ext(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ".png"
}
