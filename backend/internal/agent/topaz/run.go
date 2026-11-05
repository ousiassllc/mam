package topaz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/logging"
)

// Run does what Estimate quoted: it accepts the job (the credits are reserved), uploads the source, starts the processing, waits for it and
// downloads the result to in.DestPath. It returns the size of the result.
//
// Every error after the accept is marked modeladapter.ErrAfterSubmit: the credits are reserved and the job runs at Topaz, so it must not be
// submitted again (that would be charged twice). A refusal of the accept itself (a 4xx) is not: nothing was reserved.
func (c *Client) Run(ctx context.Context, in cloudenhance.RunInput, progress func(cloudenhance.Progress)) (int64, error) {
	key, err := c.key()
	if err != nil {
		return 0, err
	}
	// The credits are reserved by the accept: a source that is gone (or empty) is found out before it, so that nothing is reserved for a
	// job that cannot upload.
	if info, err := os.Stat(in.SourcePath); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return 0, &Error{Code: CodeInvalidInput, Message: "the source video file is missing or empty", RequestID: in.RequestID, Err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	id := url.PathEscape(in.RequestID)
	var accepted struct {
		URLs []string `json:"urls"`
	}
	if err := c.call(ctx, key, http.MethodPatch, "/video/"+id+"/accept", nil, &accepted); err != nil {
		return 0, acceptFailure(timedOut(ctx, err))
	}
	fail := func(err error) (int64, error) { return 0, modeladapter.AfterSubmit(timedOut(ctx, err)) }

	// One upload URL is what the documentation shows for a small file; the parts of a large one are not described (how the file is cut), so
	// anything else is not guessed. The job times out at Topaz (TIMEOUT_UPLOAD_SOURCE) and is refunded.
	if len(accepted.URLs) != 1 || accepted.URLs[0] == "" {
		return fail(badAnswer("urls（アップロード先が 1 つ）"))
	}
	etag, err := c.upload(ctx, in.RequestID, accepted.URLs[0], in.SourcePath)
	if err != nil {
		return fail(err)
	}
	complete := map[string]any{"uploadResults": []map[string]any{{"partNum": 1, "eTag": etag}}}
	if err := c.call(ctx, key, http.MethodPatch, "/video/"+id+"/complete-upload/", complete, nil); err != nil {
		return fail(err)
	}
	downloadURL, err := c.wait(ctx, key, in.RequestID, progress)
	if err != nil {
		return fail(err)
	}
	n, err := c.download(ctx, in.RequestID, downloadURL, in.DestPath)
	if err != nil {
		return fail(err)
	}
	return n, nil
}

// acceptFailure marks a failure of the accept that does not tell whether Topaz reserved the credits (no answer, a 5xx, the time limit). A
// 4xx is a refusal: nothing was reserved.
func acceptFailure(err error) error {
	var pe *modeladapter.ProviderError
	if errors.As(err, &pe) && pe.Status >= 400 && pe.Status < 500 {
		return err
	}
	return modeladapter.AfterSubmit(err)
}

// timedOut is err, or, when the time limit of the job (or of the caller) ended it, the timeout error that errmap tells as a time out.
func timedOut(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && !errors.Is(err, modeladapter.ErrTimeout) {
		return fmt.Errorf("the Topaz job ran past its time limit: %w", modeladapter.ErrTimeout)
	}
	return err
}

// status is the part of the answer of GET /video/{id}/status that is read. Which fields there are is not documented beyond status, download.url
// and errorCode; progress is read as a percentage when it is a number.
type status struct {
	Status string `json:"status"`
	// Progress is read only when it is a JSON number: its type is not documented, and a job that is already charged must not fail on it.
	Progress json.RawMessage `json:"progress"`
	Message  string          `json:"message"`
	Code     string          `json:"errorCode"`
	Download struct {
		URL string `json:"url"`
	} `json:"download"`
}

// wait polls the status until the job is complete and returns the download URL. A failed or cancelled job is an *Error with its errorCode.
func (c *Client) wait(ctx context.Context, key, requestID string, progress func(cloudenhance.Progress)) (string, error) {
	path := "/video/" + url.PathEscape(requestID) + "/status"
	for {
		var st status
		if err := c.repeat(ctx, func() error { st = status{}; return c.call(ctx, key, http.MethodGet, path, nil, &st) }); err != nil {
			return "", err
		}
		state := strings.ToLower(st.Status)
		if progress != nil {
			progress(cloudenhance.Progress{Status: state, Percent: percent(st.Progress)})
		}
		switch state {
		case "complete":
			if st.Download.URL == "" {
				return "", badAnswer("download.url")
			}
			return st.Download.URL, nil
		case "failed", "canceled", "cancelled":
			return "", jobFailure(state, requestID, key, st)
		}
		select {
		case <-time.After(c.cfg.PollInterval):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// jobFailure is the failure of a job whose status says it failed or was cancelled.
func jobFailure(state, requestID, key string, st status) error {
	code := st.Code
	if code == "" {
		code = CodeCanceled
		if state == "failed" {
			code = CodeUnknown
		}
	}
	return &Error{Code: code, Message: logging.Redact(st.Message, key), RequestID: requestID}
}

// percent is the progress as 0..100; -1 when the answer has none.
func percent(raw json.RawMessage) float64 {
	var p float64
	if string(raw) == "null" || json.Unmarshal(raw, &p) != nil { // null unmarshals to 0 without an error
		return -1
	}
	return min(max(p, 0), 100)
}
