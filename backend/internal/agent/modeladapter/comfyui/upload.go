package comfyui

import (
	"bytes"
	"mime/multipart"
	"net/textproto"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
)

// uploadBody is the multipart body of POST /upload/image: the file and the options that keep it in the input
// directory under exactly this name.
func uploadBody(in *modeladapter.Input, filename string) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="image"; filename="`+filename+`"`)
	h.Set("Content-Type", in.MimeType)
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, "", err
	}
	_, _ = part.Write(in.Data)
	_ = w.WriteField("type", "input")
	_ = w.WriteField("overwrite", "true")
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// extension is the file extension the server derives the media type of an uploaded input from.
func extension(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ".png"
}
