// Package errmap turns the errors that come from outside mam (ComfyUI, the model vendors, the LLM providers, the network, the disk)
// into the common error of package apperr (#1010): one code per kind of failure, the original message kept as the detail. A
// failure it cannot tell is CodeUnknown, which Report logs under a correlation id.
//
// It is below the services and the screens: a service that has a refusal of its own (a changed estimate, a locked slot) words it
// itself and wraps it as an *apperr.Error; Explain returns that error as it is.
package errmap

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/failmsg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/logging"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// maxDetailRunes bounds the detail: a vendor may answer with a page of HTML.
const maxDetailRunes = 1000

// Explain is err as the common error. An *apperr.Error in the chain is returned as it is; any other failure is classified by what
// it is (a typed error of the adapters, an HTTP status, a message ComfyUI is known to send) and keeps its text as the detail. A
// failure that is none of them is CodeUnknown. nil for nil.
func Explain(err error) *apperr.Error {
	if e, ok := Known(err); ok {
		return e
	}
	if err == nil {
		return nil
	}
	return apperr.New(apperr.CodeUnknown).WithDetail(detailOf(err)).Wrap(err)
}

// Known is Explain without the fallback: ok is false when err is none of the failures this package knows (and err is not nil).
func Known(err error) (*apperr.Error, bool) {
	if err == nil {
		return nil, false
	}
	if e, ok := apperr.As(err); ok {
		return e, true
	}
	e := classify(err)
	if e == nil {
		return nil, false
	}
	if e.Detail == "" {
		e.Detail = detailOf(err)
	}
	e.Err = err
	return e, true
}

// Report is Explain that also logs a failure it does not know: the cause goes to logger with a new correlation id (the attribute
// error_id), and the id is in the returned error, so that a person who reports the failure and the log line meet. A known failure is not
// logged here: the caller logs what it wants to log. attrs are added to the log line.
func Report(logger *slog.Logger, msg string, err error, attrs ...any) *apperr.Error {
	e := Explain(err)
	if e == nil || e.Code != apperr.CodeUnknown || e.ID != "" {
		return e
	}
	e.ID = apperr.NewID()
	if logger != nil {
		logger.Error(msg, append([]any{"error_id", e.ID, "err", err}, attrs...)...)
	}
	return e
}

// detailOf is the text of err for the detail: a secret that got into it (a URL with a key, a header dump) is hidden, and it is cut.
func detailOf(err error) string {
	d := strings.TrimSpace(logging.Redact(err.Error()))
	if utf8.RuneCountInString(d) > maxDetailRunes {
		d = string([]rune(d)[:maxDetailRunes]) + "…"
	}
	return d
}

// classify is the table of the typed failures, in the order they are told apart: what says exactly what happened first, the
// HTTP status of a vendor last (a ComfyUI that failed on the GPU answers 422 with the reason in the message).
func classify(err error) *apperr.Error {
	var (
		te           *localcfg.TimeoutError
		fetchNoSpace *fetch.NoSpaceError
		lost         *fetch.ConnectionLostError
	)
	switch {
	case errors.Is(err, llm.ErrNoAPIKey):
		return failmsg.NoKeyError(err)
	case errors.Is(err, modeladapter.ErrNoAPIKey):
		return apperr.New(apperr.CodeKeyMissing)
	case errors.Is(err, audiodirector.ErrNoAPIKey):
		return apperr.New(apperr.CodeKeyMissing).With("service", "ElevenLabs の ")
	case errors.As(err, &te):
		return localTimeout(te)
	case errors.Is(err, modeladapter.ErrTimeLimit):
		return apperr.New(apperr.CodeTimeout)
	case modeladapter.RateLimited(err): // before the breaker: the 429 that opened it is the cause the user can act on
		return apperr.New(apperr.CodeRateLimited)
	case errors.Is(err, modeladapter.ErrCircuitOpen), errors.Is(err, llm.ErrCircuitOpen):
		return apperr.New(apperr.CodeCircuitOpen)
	case errors.Is(err, chatgpt.ErrNoImageModel):
		return apperr.New(apperr.CodeImageUnavailable)
	case errors.Is(err, direct.ErrInputsUnsupported), errors.Is(err, direct.ErrInputsConflict), errors.Is(err, direct.ErrResolution):
		return apperr.New(apperr.CodeInputUnsupported)
	case errors.Is(err, llm.ErrInvalidOutput):
		return apperr.New(apperr.CodeLLMInvalidOutput)
	case errors.Is(err, fetch.ErrChecksum), errors.Is(err, fetch.ErrSize):
		return apperr.New(apperr.CodeDownloadCorrupt)
	case errors.Is(err, store.ErrNotWritable):
		return apperr.New(apperr.CodeNotWritable)
	case errors.Is(err, media.ErrTimeout):
		return apperr.New(apperr.CodeMediaTimeout)
	case errors.As(err, &fetchNoSpace):
		return apperr.New(apperr.CodeDiskFull).With("where", "{{where.path}}").With("path", fetchNoSpace.Path)
	case errors.Is(err, syscall.ENOSPC):
		return apperr.New(apperr.CodeDiskFull)
	case errors.As(err, &lost):
		return apperr.New(apperr.CodeConnectionFailed)
	}
	if e := byMessage(err.Error()); e != nil {
		return e
	}
	if status, ok := statusOf(err); ok {
		if e := byStatus(status); e != nil {
			return e
		}
	}
	return byKind(err)
}

// byKind is the failures that have no message and no status to tell more: a timeout, the folder, the network, a refusal without a status, the
// provider's own words.
func byKind(err error) *apperr.Error {
	switch {
	case errors.Is(err, modeladapter.ErrTimeout), errors.Is(err, llm.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return apperr.New(apperr.CodeTimeout)
	case errors.Is(err, fs.ErrPermission):
		return apperr.New(apperr.CodeNotWritable)
	case isNetwork(err):
		return apperr.New(apperr.CodeConnectionFailed)
	case errors.Is(err, modeladapter.ErrRejected): // the sentinel alone: no status to tell more
		return apperr.New(apperr.CodeRequestRejected)
	}
	var (
		pe  *modeladapter.ProviderError
		api *llm.APIError
	)
	switch {
	case errors.As(err, &api) && api.Notice != "": // the provider's own words (a usage limit, a sign-in to repeat)
		return apperr.New(apperr.CodeUpstream).WithCause(api.Notice)
	case errors.As(err, &pe) && pe.Status == 0: // no answer, and not a network error we know
		return apperr.New(apperr.CodeConnectionFailed)
	}
	return nil
}

// LocalTimeout is the failure of a call to an endpoint the user registered that ran past its limit: nil for any other error.
func LocalTimeout(err error) *apperr.Error {
	var te *localcfg.TimeoutError
	if !errors.As(err, &te) {
		return nil
	}
	e := localTimeout(te)
	e.Detail, e.Err = detailOf(err), err
	return e
}

func localTimeout(te *localcfg.TimeoutError) *apperr.Error {
	switch {
	case te.Stalled:
		return apperr.New(apperr.CodeStalled).With("after", duration(te.After))
	case te.After >= localcfg.MaxTimeoutSeconds*time.Second:
		return apperr.New(apperr.CodeTimeLimit).With("after", duration(te.After))
	}
	return apperr.New(apperr.CodeEndpointTimeout).With("after", duration(te.After))
}

// duration is a time limit in words: whole hours, else whole minutes, else seconds.
func duration(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return strconv.Itoa(int(d/time.Hour)) + "時間"
	case d >= time.Minute && d%time.Minute == 0:
		return strconv.Itoa(int(d/time.Minute)) + "分"
	}
	return strconv.Itoa(int(d.Round(time.Second)/time.Second)) + "秒"
}

// needs is what ComfyUI's adapter says when an input the workflow cannot run without was not sent: "the workflow needs first_frame
// (12), the request has none" (the parameter, the node).
var needs = regexp.MustCompile(`the workflow needs ([\w.]+) \(([^)]*)\)`)

// The messages ComfyUI and the Python behind it are known to send, matched on the lower-cased text. Each is one failure a person
// can fix; a message that matches none is left to the status.
var (
	oomText     = []string{"out of memory", "outofmemoryerror", "cublas_status_alloc_failed", "allocation on device", "not enough memory"}
	diskText    = []string{"no space left on device", "not enough space on the disk", "enospc", "disk full"}
	licenseText = []string{"license has to be accepted", "accept the license", "accept_terms"}
	// modelText: ComfyUI validates a loader's file name against the files it has ("Value not in list: ckpt_name: 'x.safetensors' not in [...]").
	modelText = []string{"ckpt_name", "unet_name", "vae_name", "lora_name", "clip_name", "model_name"}
	fileText  = []string{"model file not found", "safetensors file not found"}
	nodeText  = []string{"missing_node_type", "missing node type", "node type not found", "is not found in the node list"}
)

// byMessage classifies a failure by the text it carries.
func byMessage(msg string) *apperr.Error {
	if m := needs.FindStringSubmatch(msg); m != nil {
		return apperr.New(apperr.CodeInputMissing).With("param", m[1]).With("node", m[2])
	}
	low := strings.ToLower(msg)
	switch {
	case containsAny(low, oomText):
		return apperr.New(apperr.CodeVRAM)
	case containsAny(low, diskText):
		return apperr.New(apperr.CodeDiskFull)
	case containsAny(low, licenseText):
		return apperr.New(apperr.CodeLicense)
	case strings.Contains(low, "value not in list") && containsAny(low, modelText), containsAny(low, fileText):
		return apperr.New(apperr.CodeModelMissing)
	case containsAny(low, nodeText):
		return apperr.New(apperr.CodeNodeMissing)
	}
	return nil
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// statusOf is the HTTP status a vendor, the LLM provider or the audio vendor answered with.
func statusOf(err error) (int, bool) {
	var (
		pe  *modeladapter.ProviderError
		api *llm.APIError
		ap  *audiodirector.ProviderError
		he  *fetch.HTTPError
	)
	switch {
	case errors.As(err, &pe) && pe.Status != 0 && pe.Status != http.StatusOK:
		return pe.Status, true
	case errors.As(err, &api) && api.Status != 0:
		return api.Status, true
	case errors.As(err, &ap) && ap.Status != 0:
		return ap.Status, true
	case errors.As(err, &he):
		return he.Status, true
	}
	return 0, false
}

// byStatus is the failure an HTTP status stands for.
func byStatus(status int) *apperr.Error {
	st := strconv.Itoa(status)
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return apperr.New(apperr.CodeAuthFailed).With("http", "{{http.status}}").With("status", st)
	case status == http.StatusNotFound, status == http.StatusGone:
		return apperr.New(apperr.CodeRemoteNotFound)
	case status == http.StatusRequestTimeout, status == http.StatusGatewayTimeout:
		return apperr.New(apperr.CodeTimeout)
	case status == http.StatusTooManyRequests:
		return apperr.New(apperr.CodeRateLimited)
	case status >= 500:
		return apperr.New(apperr.CodeServerError).With("http", "{{http.status}}").With("status", st)
	case status >= 400:
		return apperr.New(apperr.CodeRequestRejected).With("http", "{{http.status}}").With("status", st)
	}
	return nil
}

// isNetwork reports whether err is a failure to reach the other side.
func isNetwork(err error) bool {
	var (
		ne  net.Error
		dns *net.DNSError
	)
	if errors.As(err, &ne) || errors.As(err, &dns) {
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH)
}
