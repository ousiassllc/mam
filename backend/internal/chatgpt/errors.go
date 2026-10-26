package chatgpt

import "errors"

var (
	// ErrNotSignedIn means the login system has no sign-in.
	ErrNotSignedIn = errors.New("chatgpt: not signed in")
	// ErrReloginRequired means the sign-in can no longer be refreshed (revoked, expired or reused refresh token):
	// the tokens are kept, but only a new sign-in helps.
	ErrReloginRequired = errors.New("chatgpt: the sign-in has expired, sign in again")
	// ErrPlanUsageNotGranted means the official sign-in was made without the chatgpt.tokens.use.direct scope: the
	// identity works, ChatGPT plan usage does not. Signing in again lets the user grant it.
	ErrPlanUsageNotGranted = errors.New("chatgpt: ChatGPT plan usage was not granted")
	// ErrImageDisabled means the unofficial image generation is off.
	ErrImageDisabled = errors.New("chatgpt: unofficial image generation is turned off")
	// ErrRisksNotAcknowledged means image generation was asked to be turned on without the risks confirmed.
	ErrRisksNotAcknowledged = errors.New("chatgpt: the risks of the unofficial image generation were not confirmed")
	// ErrNoDeviceLogin means the login system has no device-code login (the official client has none).
	ErrNoDeviceLogin = errors.New("chatgpt: this sign-in has no device-code login; sign in on the machine mam runs on")
)
