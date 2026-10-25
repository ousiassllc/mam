// Package protocol holds the part of the Codex app-server JSON-RPC protocol that mam uses: the method names and the
// fields of the messages it sends and reads, written by hand (not generated from the schema) so that mam depends on
// nothing else. The protocol changes with the Codex version: Messages lists, for each type, the schema it stands for,
// and the schema test compares those fields with the schema of the version the catalog pins.
//
// A message type here has no field for a token: mam never reads, keeps or returns one (account/read is asked with
// refreshToken false and answers an account, a plan and an e-mail address; the login result is a URL or a code).
package protocol

// Client requests mam sends.
const (
	MethodInitialize        = "initialize"
	MethodAccountRead       = "account/read"
	MethodLoginStart        = "account/login/start"
	MethodLoginCancel       = "account/login/cancel"
	MethodLogout            = "account/logout"
	MethodRateLimitsRead    = "account/rateLimits/read"
	MethodModelList         = "model/list"
	MethodThreadStart       = "thread/start"
	MethodThreadUnsubscribe = "thread/unsubscribe"
	MethodTurnStart         = "turn/start"
	MethodTurnInterrupt     = "turn/interrupt"
)

// NotifInitialized is the notification that ends the handshake (the only client notification).
const NotifInitialized = "initialized"

// Notifications mam reads.
const (
	NotifLoginCompleted = "account/login/completed"
	NotifAccountUpdated = "account/updated"
	NotifRateLimits     = "account/rateLimits/updated"
	NotifAgentDelta     = "item/agentMessage/delta"
	NotifItemCompleted  = "item/completed"
	NotifTurnCompleted  = "turn/completed"
	NotifError          = "error"
)

// Requests of the server that mam answers (always by refusing: appserver.Refuse).
const (
	ReqCommandApproval     = "item/commandExecution/requestApproval"
	ReqFileChangeApproval  = "item/fileChange/requestApproval"
	ReqPermissionsApproval = "item/permissions/requestApproval"
	ReqToolUserInput       = "item/tool/requestUserInput"
	ReqMCPElicitation      = "mcpServer/elicitation/request"
	ReqDynamicToolCall     = "item/tool/call"
	ReqLegacyPatchApproval = "applyPatchApproval"
	ReqLegacyExecApproval  = "execCommandApproval"
	ReqAuthTokensRefresh   = "account/chatgptAuthTokens/refresh" //nolint:gosec // a method name, not a credential
	ReqAttestation         = "attestation/generate"
)

// Login types of account/login/start. mam offers the browser and the device code only: no API key and no token
// handed to Codex (chatgptAuthTokens).
const (
	LoginBrowser    = "chatgpt"
	LoginDeviceCode = "chatgptDeviceCode"
)

// AccountTypeChatGPT is the account type of a ChatGPT login (account.type).
const AccountTypeChatGPT = "chatgpt"

// Values of the thread and the turn that keep the turn to plain text generation.
const (
	ApprovalNever   = "never"
	SandboxReadOnly = "read-only" // thread/start: the SandboxMode
	PolicyReadOnly  = "readOnly"  // turn/start: the type of the SandboxPolicy
)

// Turn status (turn.status).
const (
	TurnCompleted   = "completed"
	TurnFailed      = "failed"
	TurnInterrupted = "interrupted"
)

// Item and input types.
const (
	ItemAgentMessage = "agentMessage"
	PhaseFinalAnswer = "final_answer"
	InputText        = "text"
	InputLocalImage  = "localImage"
	ModalityImage    = "image"
)

// BucketCodex is the key of the Codex usage in the rate limits by limit id.
const BucketCodex = "codex"

// ClientInfoName and ClientInfoTitle identify mam to the app-server (clientInfo; OpenAI's compliance logs use the name).
const (
	ClientInfoName  = "mam"
	ClientInfoTitle = "MAM"
)

// Config keys and values mam passes to the app-server with -c (TOML values).
const (
	ConfigCredentialsStore = "cli_auth_credentials_store" //nolint:gosec // a config key, not a credential
	CredentialsKeyring     = "keyring"                    // the OS secure storage; fails when it is unavailable
	CredentialsAuto        = "auto"                       // the keyring when available, else a file in CODEX_HOME
	ConfigWebSearch        = "web_search"
	WebSearchDisabled      = "disabled"
)

// ToolSettings are the -c settings that switch off the tools of the agent that a text answer never needs (web search,
// apps, sub-agents, image generation, the browser). They are best effort: the config schema has no switch for the shell
// tool, which the read-only sandbox, the refusal of every approval and appserver's tool detection hold back instead.
var ToolSettings = []string{
	ConfigWebSearch + `="` + WebSearchDisabled + `"`,
	"features.apps=false",
	"features.collab=false",
	"features.image_generation=false",
	"features.browser_use=false",
}
