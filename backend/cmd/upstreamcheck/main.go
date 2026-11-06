// Command upstreamcheck asks the upstream of every URL that the model catalog pins (weights, packages, engine builds) whether it is still
// there with the pinned size and, where the upstream says so without a download, the pinned SHA-256
// (spec/architecture/model-manager/local-runtime.md §12.10). The files behind a Hugging Face login are asked only when HF_TOKEN is set. It exits 1 when a pin no longer holds. The scheduled workflow
// .github/workflows/upstream-check.yml runs it; it runs the same way on a developer's machine (make upstream-check).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/upstream"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "upstreamcheck:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("upstreamcheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	concurrency := fs.Int("concurrency", 8, "requests in flight")
	timeout := fs.Duration("timeout", 60*time.Second, "limit of one request")
	downloadTimeout := fs.Duration("download-timeout", 10*time.Minute, "limit of one download for -hash-below")
	retries := fs.Int("retries", 2, "repeats of a request that failed on the network or with HTTP 429 / 5xx")
	hashBelow := fs.Int64("hash-below", 16<<20, "download and hash a file whose upstream gives no SHA-256 when it is at most this many bytes (0: never)")
	only := fs.String("only", "", "check only the pins whose catalog place or URL contains this text")
	verbose := fs.Bool("v", false, "also list the pins that were not verified by SHA-256 (size only, alive only, skipped)")
	list := fs.Bool("list", false, "list the pins and do not ask the upstream")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pins := upstream.Pins(catalog.Default())
	if *only != "" {
		kept := pins[:0]
		for _, p := range pins {
			if strings.Contains(p.Label, *only) || strings.Contains(p.URL, *only) {
				kept = append(kept, p)
			}
		}
		pins = kept
	}
	if *list {
		for _, p := range pins {
			fmt.Fprintf(stdout, "%s\t%d\t%s\t%s\n", p.SHA256, p.Size, p.URL, p.Label)
		}
		return nil
	}

	checker := upstream.Checker{
		Client:          httpclient.New(),
		Concurrency:     *concurrency,
		Timeout:         *timeout,
		DownloadTimeout: *downloadTimeout,
		Retries:         *retries,
		RetryDelay:      5 * time.Second,
		HashBelow:       *hashBelow,
		HFToken:         getenv("HF_TOKEN"),
	}
	if s := upstream.WriteReport(stdout, checker.Run(ctx, pins), *verbose); s.Failed > 0 {
		return fmt.Errorf("%d of %d pinned downloads no longer match their upstream", s.Failed, s.Total)
	}
	return nil
}
