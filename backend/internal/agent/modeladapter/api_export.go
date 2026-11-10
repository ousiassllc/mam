package modeladapter

import "context"

// API is the HTTP client of one vendor's backend; the backends of other packages (providerbackends) call these methods, which are
// api's own helpers: Key is the stored API key, Request a request to an absolute URL, Poll waits for a job (its errors are marked
// AfterSubmit), Download stages a result video (auth: the headers for the vendor's own host), Decode reads a JSON answer, Empty and
// Failed are the errors of an answer without a result and of a job that ended without one.
type API = api

func (a api) BaseURL() string                 { return a.baseURL }
func (a api) Key() (string, error)            { return a.key() }
func (a api) Decode(d []byte, into any) error { return unmarshal(a, d, into) }
func (a api) Empty(what string) error         { return a.empty(what) }
func (a api) Failed(reason string) error      { return a.failed(reason) }
func (a api) Request(ctx context.Context, method, target string, header map[string]string, body []byte) ([]byte, error) {
	return a.request(ctx, method, target, header, body, maxJSONBytes)
}
func (a api) Poll(ctx context.Context, check func(context.Context) (bool, error)) error {
	return a.poll(ctx, check)
}
func (a api) Download(ctx context.Context, fileURL string, auth map[string]string) (Blob, string, error) {
	if len(auth) > 0 {
		return a.downloadAuthed(ctx, fileURL, auth)
	}
	return a.download(ctx, fileURL)
}

// VideoMime is the video type of a Content-Type header ("video/mp4" for any other).
func VideoMime(header string) string { return videoMime(header) }
