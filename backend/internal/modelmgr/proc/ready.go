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
		res, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
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
