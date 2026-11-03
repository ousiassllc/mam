package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidOutput means the model answered, but not with the JSON the Agent asked for.
var ErrInvalidOutput = errors.New("llm: the answer is not the requested JSON")

// MaxJSONRetries is how many times a local model is asked again when its answer is not the JSON the Agent asked for (so it is called up
// to 1 + MaxJSONRetries times). A model on this PC is the one that often breaks the form (a sentence before the object, a cut-off
// object); the providers of the cloud are not asked again, as before.
const MaxJSONRetries = 2

// CompleteJSON asks the Agent's model for a JSON object and decodes it into T. Models wrap
// JSON in code fences or a sentence of prose; the outermost object is extracted first.
func CompleteJSON[T any](ctx context.Context, c Completer, agent Agent, req Request) (T, error) {
	out, _, err := CompleteJSONResponse[T](ctx, c, agent, req)
	return out, err
}

// CompleteJSONResponse is CompleteJSON that also returns the Response (the target that answered and the tokens it used), for the
// callers that account for the call. The Response is returned with a decoding error too: the call was made and paid for.
//
// An answer of a local model (ProviderLocal) that cannot be decoded is asked for again, at most MaxJSONRetries times, with the reason
// added to the prompt; the Response then holds the tokens of every call. The error of a failure says which model, how many calls, and why
// the last answer could not be read.
func CompleteJSONResponse[T any](ctx context.Context, c Completer, agent Agent, req Request) (T, Response, error) {
	var out, zero T
	req.JSON = true
	res, err := c.Complete(ctx, agent, req)
	if err != nil {
		return out, res, err
	}
	total, attempts := res, 1
	derr := decodeJSON(res.Text, &out)
	for derr != nil && res.Provider == ProviderLocal && attempts <= MaxJSONRetries {
		again := req
		again.User = req.User + retryNote(res.Text, derr)
		next, err := c.Complete(ctx, agent, again)
		if err != nil {
			return zero, total, err
		}
		attempts++
		next.Usage = sumUsage(total.Usage, next.Usage)
		res, total = next, next
		out = zero
		derr = decodeJSON(res.Text, &out)
	}
	if derr != nil {
		if attempts == 1 {
			return zero, res, fmt.Errorf("%w (%s/%s): %w", ErrInvalidOutput, res.Provider, res.Model, derr)
		}
		return zero, res, fmt.Errorf("%w (%s/%s, %d calls): %s: %w", ErrInvalidOutput, res.Provider, res.Model, attempts, whyUnreadable(res.Text), derr)
	}
	return out, res, nil
}

func decodeJSON(text string, out any) error {
	return json.Unmarshal([]byte(extractJSONObject(text)), out)
}

// retryNote is added to the prompt of the call that follows an answer that could not be read.
func retryNote(text string, derr error) string {
	return "\n\n(Your previous answer could not be read as JSON: " + whyUnreadable(text) + " Answer again with the JSON object only: " +
		"no explanation, no code fence, and nothing before or after it. The decoder said: " + derr.Error() + ")"
}

// whyUnreadable is why the text of an answer is not a JSON object, in one sentence.
func whyUnreadable(text string) string {
	switch trimmed := strings.TrimSpace(text); {
	case trimmed == "":
		return "the answer is empty."
	case !strings.Contains(trimmed, "{"):
		return "the answer has no JSON object in it."
	case !strings.Contains(trimmed, "}"):
		return "the JSON object is cut off (it never closes); the answer may have run out of tokens."
	}
	return "the text between the first '{' and the last '}' is not valid JSON."
}

// sumUsage adds the tokens of two calls (nil when neither reports any).
func sumUsage(a, b *Usage) *Usage {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	return &Usage{InputTokens: a.InputTokens + b.InputTokens, OutputTokens: a.OutputTokens + b.OutputTokens}
}

// extractJSONObject returns the text from the first '{' to the last '}' (the whole text when there is none).
func extractJSONObject(s string) string {
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return strings.TrimSpace(s)
	}
	return s[start : end+1]
}
