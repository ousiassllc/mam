package modelnodes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// loadedOverHTTP asks the engine at baseURL (loopback) for each class: GET /object_info/<class> answers an object with the class as its key
// when ComfyUI has the node, and {} when it has not.
func loadedOverHTTP(ctx context.Context, baseURL string, classes []string) ([]string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	var missing []string
	for _, class := range classes {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/object_info/"+url.PathEscape(class), http.NoBody)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req) // #nosec G107 G704 -- the loopback address of the engine mam started
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("object_info/%s answered %d", class, resp.StatusCode)
		}
		var out map[string]json.RawMessage
		if json.Unmarshal(body, &out) != nil {
			return nil, fmt.Errorf("object_info/%s: undecodable answer", class)
		}
		if _, ok := out[class]; !ok {
			missing = append(missing, class)
		}
	}
	return missing, nil
}
