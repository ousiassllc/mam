package fetch

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

// allowedDomains are the hosts of the downloads the model manager makes, and their subdomains: Hugging Face
// (the model files and its CDN, which is cdn-lfs*.huggingface.co and *.hf.co) and GitHub (the engine archive,
// which redirects to *.githubusercontent.com). Nothing else is fetched.
var allowedDomains = []string{"huggingface.co", "hf.co", "github.com", "githubusercontent.com"}

// hfDomains are the hosts of the Hugging Face import (the API, the files and the CDN): no GitHub, so that a redirect
// from Hugging Face never leaves it.
var hfDomains = []string{"huggingface.co", "hf.co"}

// packageDomains are the hosts a local runtime that mam puts together itself (MuseTalk) fetches its parts from, on top of
// allowedDomains: python.org (the embeddable Python), pythonhosted.org (the wheels of PyPI), pytorch.org (PyTorch's wheels; its
// CDN is a subdomain) and openmmlab.com (mmcv's wheels). Every such download is pinned by SHA-256 and size in the catalog, and the
// catalog's URL patterns (catalog.PartURLAllowed) are narrower than these domains.
var packageDomains = []string{"python.org", "pythonhosted.org", "pytorch.org", "openmmlab.com"}

// AllowedURL accepts an https URL on an allowed host (and, through safefetch, no address of the local network).
func AllowedURL(u *url.URL) error { return checkHost(u, allowedDomains) }

// PackageURL accepts an https URL on the hosts of AllowedURL and of the Python packages of an assembled runtime (see
// packageDomains), and, through safefetch, no address of the local network. The install of a runtime package uses it; the
// weights keep AllowedURL.
func PackageURL(u *url.URL) error {
	return checkHost(u, append(slices.Clone(allowedDomains), packageDomains...))
}

// HFAllowedURL accepts an https URL on huggingface.co or hf.co and their subdomains only (and no address of the local
// network). The Hugging Face client and the download of an imported file use it; the engine download keeps AllowedURL.
func HFAllowedURL(u *url.URL) error { return checkHost(u, hfDomains) }

// AllowHosts is the URL check of a download from the given domains (and their subdomains) only: https, and no address of the
// local network. A catalog of programs that mam fetches (ffmpeg) names the hosts of its pinned URLs this way.
func AllowHosts(domains ...string) func(*url.URL) error {
	return func(u *url.URL) error { return checkHost(u, domains) }
}

func checkHost(u *url.URL, domains []string) error {
	if err := safefetch.CheckURL(u.String()); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	for _, d := range domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return nil
		}
	}
	return fmt.Errorf("fetch: %s is not a download host of the model manager", host)
}

const maxRedirects = 8

// NewClient returns the HTTP client of the downloads: it connects to public addresses only (safefetch), follows
// redirects that pass check, and takes the Authorization header off a request that leaves the host the
// download started on (the token goes to authHost's first request only; the CDN is addressed by a signed URL).
func NewClient(check func(*url.URL) error, authHost string) *http.Client {
	return WithRedirectRules(safefetch.NewClient(), check, authHost)
}

// WithRedirectRules sets the redirect rules of NewClient on c (a test passes a client that may reach loopback).
func WithRedirectRules(c *http.Client, check func(*url.URL) error, authHost string) *http.Client {
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errors.New("fetch: too many redirects")
		}
		if err := check(req.URL); err != nil {
			return err
		}
		if req.URL.Host != via[0].URL.Host || !strings.EqualFold(req.URL.Hostname(), authHost) {
			req.Header.Del("Authorization")
		}
		return nil
	}
	return c
}
