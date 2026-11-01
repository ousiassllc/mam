package proc

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// HTTPReady is a Ready that is satisfied when GET <url><path> answers 200 (the default is "/system_stats", ComfyUI's).
func HTTPReady(path string) func(ctx context.Context, url string) error {
	return func(ctx context.Context, url string) error {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+path, nil)
		if err != nil {
			return err
		}
		// One probe per client: a kept connection would be left open with nobody to reuse it.
		res, err := (&http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}).Do(req)
		if err != nil {
			return err
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("status %d", res.StatusCode)
		}
		return nil
	}
}
