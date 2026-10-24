package workflows

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

// NodeState is whether the node screen can be embedded.
type NodeState string

// The states of a NodeTarget.
const (
	// NodeReady: the ComfyUI answers and the iframe can open it.
	NodeReady NodeState = "ready"
	// NodeStopped: the bundled engine is installed but not running (Start starts it).
	NodeStopped NodeState = "stopped"
	// NodeStarting: the bundled engine is starting.
	NodeStarting NodeState = "starting"
	// NodeFailed: the bundled engine failed to start or ended; Reason has the end of its log.
	NodeFailed NodeState = "failed"
	// NodeNotInstalled: the bundled engine is not installed: the model manager installs it.
	NodeNotInstalled NodeState = "not_installed"
	// NodeUnsupported: this build has no bundled engine (not Windows), so use an external ComfyUI.
	NodeUnsupported NodeState = "unsupported"
	// NodeUnreachable: an external ComfyUI does not answer.
	NodeUnreachable NodeState = "unreachable"
	// NodeRejected: an external ComfyUI refuses to be shown inside another site (its origin check): add
	// --enable-cors-header <mam origin> to it, or open it in the browser.
	NodeRejected NodeState = "rejected"
)

// NodeTarget is the ComfyUI the node screen shows and how: the address of the iframe, the origin the CSP of the
// screen must allow (middleware.AllowFrame; nothing else is ever allowed), and, when it cannot be shown, why.
type NodeTarget struct {
	// Endpoint is the registered ComfyUI endpoint; Bundled is true for the engine mam manages.
	Endpoint string
	Bundled  bool
	State    NodeState
	// Reason says (for the user) why the screen cannot be shown; empty when State is NodeReady.
	Reason string
	// Origin is the CSP frame-src source ("http://127.0.0.1:8188"); FrameURL the iframe's src (Origin + "/"); OpenURL the
	// same address for "ブラウザで開く". All are empty unless the ComfyUI has an address.
	Origin   string
	FrameURL string
	OpenURL  string
	// MamOrigin is this screen's own origin, the value --enable-cors-header takes (rejected only).
	MamOrigin string
	// Installed: the bundled engine is installed (the screen offers to start it).
	Installed bool
}

// probeTimeout bounds the reads the node screen makes of the ComfyUI before it draws the page.
const probeTimeout = 5 * time.Second

// NodeTarget resolves the ComfyUI the node screen of the request (Host header requestHost) shows. endpoint is a
// registered ComfyUI endpoint; "" selects the bundled one. The bundled engine is not started here (Start does).
//
// An iframe to the bundled engine uses the same literal host as the page (127.0.0.1 or localhost): ComfyUI refuses
// requests the browser calls cross-site, and 127.0.0.1 and localhost are different sites.
func (s *Service) NodeTarget(ctx context.Context, requestHost, endpoint string) NodeTarget {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = BundledEndpoint
	}
	t := NodeTarget{Endpoint: endpoint, Bundled: endpoint == BundledEndpoint, MamOrigin: "http://" + requestHost}
	if t.Bundled {
		return s.bundledTarget(requestHost, t)
	}
	return s.externalTarget(ctx, requestHost, t)
}

func (s *Service) bundledTarget(requestHost string, t NodeTarget) NodeTarget {
	if s.Engine == nil {
		t.State, t.Reason = NodeUnsupported, "この mam には同梱の ComfyUI がありません。外部の ComfyUI を接続先に登録してください"
		return t
	}
	e := s.Engine.Engine()
	t.Installed = e.Installed
	switch {
	case !e.Supported:
		t.State, t.Reason = NodeUnsupported, "同梱の ComfyUI は Windows 版だけで使えます。外部の ComfyUI を接続先に登録して選んでください"
	case !e.Installed:
		t.State, t.Reason = NodeNotInstalled, "同梱の ComfyUI がまだ入っていません。モデルマネージャーで入れてください"
	case e.Process.State == proc.StateRunning:
		origin, ok := sameHostOrigin(e.Process.URL, requestHost)
		if !ok {
			t.State, t.Reason = NodeFailed, "同梱の ComfyUI のアドレスを読めません: "+e.Process.URL
			return t
		}
		t.State, t.Origin = NodeReady, origin
	case e.Process.State == proc.StateStarting:
		t.State, t.Reason = NodeStarting, "ComfyUI を起動しています。しばらく待ってから開き直してください"
	case e.Process.State == proc.StateFailed:
		t.State, t.Reason = NodeFailed, "ComfyUI を起動できませんでした。"+e.Process.Error
	default:
		t.State, t.Reason = NodeStopped, "ComfyUI は止まっています"
	}
	return t.withURLs()
}

func (s *Service) externalTarget(ctx context.Context, requestHost string, t NodeTarget) NodeTarget {
	ep, ok := s.Adapter.Config().Endpoints[t.Endpoint]
	if !ok {
		t.State, t.Reason = NodeUnreachable, "接続先 "+t.Endpoint+" は登録されていません"
		return t
	}
	origin, ok := originOf(ep.BaseURL)
	if !ok {
		t.State, t.Reason = NodeUnreachable, "接続先 "+t.Endpoint+" の URL を読めません"
		return t
	}
	t.Origin = origin
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	c, err := s.client(ctx, t.Endpoint, false)
	if err != nil {
		t.State, t.Reason = NodeUnreachable, reasonOf(err)
		return t.withURLs()
	}
	if _, err := c.Request(ctx, http.MethodGet, "/system_stats", nil, nil, 1<<20); err != nil {
		t.State, t.Reason = NodeUnreachable, "ComfyUI（"+origin+"）に接続できません: "+reasonOf(err)
		return t.withURLs()
	}
	// What a browser sends when this page (another site than the ComfyUI) opens it in an iframe: ComfyUI answers 403
	// to Sec-Fetch-Site: cross-site unless it was started with --enable-cors-header. A server-side request may
	// carry the same header, so the refusal is known before the iframe is drawn.
	if !sameSite(requestHost, origin) {
		_, err := c.Request(ctx, http.MethodGet, "/", map[string]string{
			"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "iframe", "Origin": t.MamOrigin,
		}, nil, 1<<20)
		if status := statusOf(err); status == http.StatusForbidden {
			t.State = NodeRejected
			t.Reason = "この ComfyUI は、別のサイト（mam）の中に表示されることを拒否しています（403）。ComfyUI を起動するときに --enable-cors-header " +
				t.MamOrigin + " を付けるか、下のリンクからブラウザで開いてください"
			return t.withURLs()
		}
	}
	t.State = NodeReady
	return t.withURLs()
}

// Start starts the bundled ComfyUI (a no-op when it runs) and returns when it answers.
func (s *Service) Start(ctx context.Context) error {
	if s.Engine == nil {
		return invalid("この mam には同梱の ComfyUI がありません")
	}
	_, err := s.client(ctx, BundledEndpoint, true)
	return err
}

func (t NodeTarget) withURLs() NodeTarget {
	if t.Origin != "" {
		t.FrameURL, t.OpenURL = t.Origin+"/", t.Origin+"/"
	}
	return t
}

// statusOf is the HTTP status a ProviderError carries (0 for any other error).
func statusOf(err error) int {
	var pe *modeladapter.ProviderError
	if errors.As(err, &pe) {
		return pe.Status
	}
	return 0
}

// originOf is "scheme://host[:port]" of a URL.
func originOf(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	return u.Scheme + "://" + u.Host, true
}

// sameHostOrigin is the origin of the engine's URL with the host of the request when that is "localhost" or a 127.x
// address (the engine listens on 127.0.0.1, which all of them reach); any other request host keeps the engine's own.
func sameHostOrigin(engineURL, requestHost string) (string, bool) {
	u, err := url.Parse(engineURL)
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	host := u.Hostname()
	if h := hostOf(requestHost); h == "localhost" || isLoopback4(h) {
		host = h
	}
	return u.Scheme + "://" + net.JoinHostPort(host, u.Port()), true
}

func hostOf(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.ToLower(strings.Trim(h, "[]"))
	}
	return strings.ToLower(strings.Trim(hostport, "[]"))
}

func isLoopback4(host string) bool {
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Is4() && ip.IsLoopback()
}

// sameSite is whether the origin is the same site as the page served at requestHost, as the browser decides for
// Sec-Fetch-Site: both are IP addresses or names, and these are the same host (the scheme of both is http).
func sameSite(requestHost, origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "http" && strings.EqualFold(u.Hostname(), hostOf(requestHost))
}
