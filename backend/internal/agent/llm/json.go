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

// CompleteJSON asks the Agent's model for a JSON object and decodes it into T. Models wrap
// JSON in code fences or a sentence of prose; the outermost object is extracted first.
func CompleteJSON[T any](ctx context.Context, c Completer, agent Agent, req Request) (T, error) {
	out, _, err := CompleteJSONResponse[T](ctx, c, agent, req)
	return out, err
}

// CompleteJSONResponse is CompleteJSON that also returns the Response (the target that answered and the tokens it used), for the
// callers that account for the call. The Response is returned with a decoding error too: the call was made and paid for.
func CompleteJSONResponse[T any](ctx context.Context, c Completer, agent Agent, req Request) (T, Response, error) {
	var out T
	req.JSON = true
	res, err := c.Complete(ctx, agent, req)
	if err != nil {
		return out, res, err
	}
	if err := json.Unmarshal([]byte(extractJSONObject(res.Text)), &out); err != nil {
		var zero T
		return zero, res, fmt.Errorf("%w (%s/%s): %w", ErrInvalidOutput, res.Provider, res.Model, err)
	}
	return out, res, nil
}

// extractJSONObject returns the text from the first '{' to the last '}' (the whole text when there is none).
func extractJSONObject(s string) string {
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return strings.TrimSpace(s)
	}
	return s[start : end+1]
}
