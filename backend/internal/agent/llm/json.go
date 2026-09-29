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
	var out T
	req.JSON = true
	res, err := c.Complete(ctx, agent, req)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(extractJSONObject(res.Text)), &out); err != nil {
		var zero T
		return zero, fmt.Errorf("%w (%s/%s): %w", ErrInvalidOutput, res.Provider, res.Model, err)
	}
	return out, nil
}

// extractJSONObject returns the text from the first '{' to the last '}' (the whole text when there is none).
func extractJSONObject(s string) string {
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return strings.TrimSpace(s)
	}
	return s[start : end+1]
}
