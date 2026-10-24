package chatgpt

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/oauth"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/tokenstore"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// LockFileName is the cross-process refresh lock, in the per-user directory of mam (not in --data-dir: the tokens in
// the OS storage are shared by every data directory).
const LockFileName = ".auth-refresh.lock"

// Options build a Service.
type Options struct {
	// Keys is the OS secure storage that holds the tokens.
	Keys credential.Store
	// DataDir holds SettingsFile.
	DataDir string
	// LockPath is the refresh lock file; "" selects <user config dir>/mam/.auth-refresh.lock.
	LockPath string
	// Issuer is the authorization server; "" selects https://auth.openai.com (tests only).
	Issuer string
	// APIBase is the OpenAI API origin of the model list; "" selects https://api.openai.com (tests only).
	APIBase string
	// HTTP is the client of every request; nil selects httpclient.New().
	HTTP httpclient.Doer
	// Open opens a URL in the default browser of the machine mam runs on; nil opens none.
	Open   func(url string) error
	Logger *slog.Logger
	// Now and Sleep replace the clock and the waits (tests).
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

// Service is both login systems and the switch of the image generation.
type Service struct {
	official *Account
	image    *Account
	settings *settings
	apiBase  string
	client   *oauth.Client
	onImage  atomic.Pointer[func()]
}

// DefaultAPIBase is the OpenAI API origin.
const DefaultAPIBase = "https://api.openai.com"

// New builds the Service.
func New(o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	client := oauth.NewClient(o.HTTP)
	if o.Now != nil {
		client.Now = o.Now
	}
	if o.Sleep != nil {
		client.Sleep = o.Sleep
	}
	if o.LockPath == "" {
		o.LockPath = DefaultLockPath()
	}
	if o.APIBase == "" {
		o.APIBase = DefaultAPIBase
	}
	account := func(kind Kind) *Account {
		return newAccount(accountOptions{
			kind: kind, issuer: o.Issuer, store: tokenstore.New(o.Keys, string(kind)), client: client,
			lockPath: o.LockPath, open: o.Open, logger: o.Logger,
		})
	}
	return &Service{
		official: account(KindOfficial), image: account(KindImage),
		settings: &settings{path: filepath.Join(o.DataDir, SettingsFile), now: client.Now},
		apiBase:  o.APIBase, client: client,
	}
}

// DefaultLockPath is <user config dir>/mam/.auth-refresh.lock, the directory config.defaultDataDir uses. Without a
// user config directory the lock falls back to the temporary directory (the lock is still shared by the user's processes).
func DefaultLockPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "mam", LockFileName)
}

// Official is the ChatGPT plan usage sign-in (text generation).
func (s *Service) Official() *Account { return s.official }

// Image is the Codex-client sign-in of the unofficial image generation. Use StartImage* to log in: they refuse while
// the generation is off.
func (s *Service) Image() *Account { return s.image }

// ImageEnabled is whether the unofficial image generation is on. It is off until the user confirmed the risks.
func (s *Service) ImageEnabled() bool { return s.settings.imageEnabled() }

// SetImageEnabled turns the unofficial image generation on or off. Turning it on needs acknowledged: the user saw
// ImageRisks and confirmed. Turning it off keeps the sign-in (log out to remove it) and stops every call at once.
func (s *Service) SetImageEnabled(enabled, acknowledged bool) error {
	if enabled && !acknowledged {
		return ErrRisksNotAcknowledged
	}
	if err := s.settings.setImage(enabled); err != nil {
		return err
	}
	if fn := s.onImage.Load(); fn != nil {
		(*fn)()
	}
	return nil
}

// SetOnImageChange registers fn, called after every successful SetImageEnabled: the image model is listed for
// selection only while the switch is on, so whoever publishes the model list refreshes it there.
func (s *Service) SetOnImageChange(fn func()) { s.onImage.Store(&fn) }

// StartImageBrowserLogin starts the browser login of the image generation; the login is for an opt-in feature and
// refuses while it is off.
func (s *Service) StartImageBrowserLogin() (Login, error) {
	if !s.ImageEnabled() {
		return Login{}, ErrImageDisabled
	}
	return s.image.StartBrowserLogin()
}

// StartImageDeviceLogin starts the device-code login of the image generation (refused while it is off).
func (s *Service) StartImageDeviceLogin(ctx context.Context) (Login, error) {
	if !s.ImageEnabled() {
		return Login{}, ErrImageDisabled
	}
	return s.image.StartDeviceLogin(ctx)
}

// ImageCredentials returns the credentials for one image call, or ErrImageDisabled without touching the network or the
// token store: the OFF switch is checked first, at every call.
func (s *Service) ImageCredentials(ctx context.Context) (Credentials, error) {
	if !s.ImageEnabled() {
		return Credentials{}, ErrImageDisabled
	}
	return s.image.Credentials(ctx)
}

// Overview is the state the screen and the MCP tools show of both login systems.
type Overview struct {
	Official Status
	Image    Status
	// ImageEnabled is the switch of the unofficial image generation.
	ImageEnabled bool
}

// Overview reads both sign-ins and the switch.
func (s *Service) Overview() (Overview, error) {
	o, err := s.official.Status()
	if err != nil {
		return Overview{}, err
	}
	i, err := s.image.Status()
	if err != nil {
		return Overview{}, err
	}
	return Overview{Official: o, Image: i, ImageEnabled: s.ImageEnabled()}, nil
}
