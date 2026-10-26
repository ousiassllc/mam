package chatgptsub

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

type errorBody struct {
	Code    string `json:"code"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (e *errorBody) code() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return e.Code
	}
	return e.Type
}

type event struct {
	Type    string     `json:"type"`
	Delta   string     `json:"delta"`
	Code    string     `json:"code"`
	Message string     `json:"message"`
	Error   *errorBody `json:"error"`
	// Response is the response object of response.completed / failed / incomplete.
	Response *struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Error             *errorBody `json:"error"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	} `json:"response"`
}

// readStream reads the server-sent events until a terminal one. Only response.completed is a success: a failure, an
// incomplete response and a stream that ends without a terminal event are three different errors.
func readStream(r io.Reader) (string, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	var text strings.Builder
	for {
		line, err := br.ReadString('\n')
		if data, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "data:"); ok {
			if out, done, derr := handle(strings.TrimSpace(data), &text); done {
				return out, derr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", &llm.APIError{Provider: provider, Err: errors.New("the stream ended without response.completed")}
			}
			return "", &llm.APIError{Provider: provider, Err: err}
		}
	}
}

// handle processes one data line. done is true for a terminal event, with the text or the error.
func handle(data string, text *strings.Builder) (out string, done bool, err error) {
	if data == "" || data == "[DONE]" {
		return "", false, nil
	}
	var ev event
	if json.Unmarshal([]byte(data), &ev) != nil {
		return "", false, nil // a line that is not an event of ours is skipped
	}
	switch ev.Type {
	case "response.output_text.delta":
		text.WriteString(ev.Delta)
	case "response.completed":
		answer := text.String()
		if answer == "" && ev.Response != nil { // a server that sent no deltas puts the text in the response object
			var b strings.Builder
			for _, o := range ev.Response.Output {
				for _, c := range o.Content {
					if c.Type == "output_text" {
						b.WriteString(c.Text)
					}
				}
			}
			answer = b.String()
		}
		if answer == "" {
			return "", true, &llm.APIError{Provider: provider, Status: http.StatusOK, Message: "the answer has no text"}
		}
		return answer, true, nil
	case "response.failed":
		var e *errorBody
		if ev.Response != nil {
			e = ev.Response.Error
		}
		return "", true, streamFailure(e)
	case "response.incomplete":
		reason := "unknown"
		if ev.Response != nil && ev.Response.IncompleteDetails != nil && ev.Response.IncompleteDetails.Reason != "" {
			reason = ev.Response.IncompleteDetails.Reason
		}
		return "", true, &llm.APIError{Provider: provider, Status: http.StatusOK, Permanent: true, Message: "the response is incomplete: " + reason}
	case "error":
		e := ev.Error
		if e == nil {
			e = &errorBody{Code: ev.Code, Message: ev.Message}
		}
		return "", true, streamFailure(e)
	}
	return "", false, nil
}

// streamFailure maps an error that arrived inside a 200 stream.
func streamFailure(e *errorBody) error {
	return classify(0, e.code(), messageOf(e), 0)
}

func messageOf(e *errorBody) string {
	if e == nil {
		return ""
	}
	return e.Message
}

// httpError maps a non-2xx answer: {"error":{"code":…}} for the structured errors, {"detail":…} before the stream.
func httpError(res *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var env struct {
		Error  *errorBody `json:"error"`
		Detail string     `json:"detail"`
	}
	_ = json.Unmarshal(body, &env)
	message := messageOf(env.Error)
	if message == "" {
		message = env.Detail
	}
	if message == "" {
		message = httpclient.Snippet(body)
	}
	return classify(res.StatusCode, env.Error.code(), message, httpclient.ParseRetryAfter(res.Header.Get("Retry-After")))
}

// classify is the error table of the flow (SIWC errors-and-recovery). status is 0 for an error inside the stream.
// Not exhaustive: an unknown code keeps its HTTP status (a 4xx is not retried, a 5xx is) and, inside a stream, is
// retried only when the code names a server-side trouble.
func classify(status int, code, message string, retryAfter time.Duration) *llm.APIError {
	e := &llm.APIError{Provider: provider, Status: status, Message: joinCode(code, message), RetryAfter: retryAfter}
	switch code {
	case "subscription_sharing_usage_limit_exceeded":
		e.Status, e.Permanent = http.StatusTooManyRequests, true
		e.Notice = "ChatGPT のプランの利用上限に達しました。利用状況: " + chatgpt.UsageURL
	case "subscription_sharing_user_not_eligible":
		e.Status, e.Permanent = http.StatusForbidden, true
		e.Notice = "この ChatGPT アカウントではプランの利用が認められていません。利用状況: " + chatgpt.UsageURL
	case "subscription_sharing_usage_unavailable", "subscription_sharing_user_unavailable":
		e.Status = http.StatusServiceUnavailable
	case "subscription_sharing_unsupported_capability":
		e.Status, e.Permanent = http.StatusBadRequest, true // a bug of the request: waiting does not help
	case "subscription_sharing_route_not_supported":
		e.Status, e.Permanent = http.StatusForbidden, true // a bug of the method or endpoint
	case "subscription_sharing_invalid_user":
		e.Status, e.Permanent = http.StatusUnauthorized, true
		e.Notice = "ChatGPT のログインが無効になっています。設定画面でログインし直してください。"
	case "chatpass_v2_scope_not_authorized", "chatpass_v2_invalid_authorization_context":
		e.Status, e.Permanent = http.StatusForbidden, true
		e.Notice = "ChatGPT のプランの利用が許可されていません。設定画面でログインし直し、許可してください。"
	default:
		if status == 0 { // inside a stream there is no status: only a server-side code is worth another try
			if serverSide(code) {
				e.Status = http.StatusServiceUnavailable
			} else {
				e.Status, e.Permanent = http.StatusUnprocessableEntity, true
			}
		}
	}
	return e
}

func serverSide(code string) bool {
	switch code {
	case "server_error", "service_unavailable", "overloaded", "internal_error":
		return true
	}
	return false
}

func joinCode(code, message string) string {
	switch {
	case code == "":
		return message
	case message == "":
		return code
	}
	return code + ": " + message
}
