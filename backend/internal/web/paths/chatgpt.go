package paths

// ChatGPTSettings is "/settings/chatgpt", the "ChatGPT でログイン" section of the settings screen (the section itself is
// part of GET /settings; GET on this path answers the section alone, which the open login polls).
func ChatGPTSettings() string { return Settings() + "/chatgpt" }

// ChatGPTAccount is "/settings/chatgpt/<kind>", one login system ("official" or "image"); DELETE signs out of it.
func ChatGPTAccount(kind string) string { return ChatGPTSettings() + "/" + kind }

// ChatGPTLogin is "/settings/chatgpt/<kind>/login": POST starts a login (field method: browser or device).
func ChatGPTLogin(kind string) string { return ChatGPTAccount(kind) + "/login" }

// ChatGPTLoginCancel is "/settings/chatgpt/<kind>/login/cancel": POST stops the login in progress.
func ChatGPTLoginCancel(kind string) string { return ChatGPTLogin(kind) + "/cancel" }

// ChatGPTImageEnabled is "/settings/chatgpt/image/enabled", the switch of the unofficial image generation (PUT).
func ChatGPTImageEnabled() string { return ChatGPTAccount("image") + "/enabled" }

// ChatGPTModels is "/settings/chatgpt/models", the models of the signed-in plan (GET).
func ChatGPTModels() string { return ChatGPTSettings() + "/models" }

// ChatGPTCodex is "/settings/chatgpt/codex", the Codex login system (the app-server route for any plan, workspace
// accounts included). The login, its cancel and the logout use the same shapes as the other systems
// (ChatGPTLogin("codex"), ChatGPTLoginCancel("codex"), ChatGPTAccount("codex")).
func ChatGPTCodex() string { return ChatGPTAccount("codex") }

// ChatGPTCodexInstall is "/settings/chatgpt/codex/install": POST downloads the pinned Codex app-server.
func ChatGPTCodexInstall() string { return ChatGPTCodex() + "/install" }

// ChatGPTCodexInstallCancel is "/settings/chatgpt/codex/install/cancel": POST stops the download (it resumes later).
func ChatGPTCodexInstallCancel() string { return ChatGPTCodexInstall() + "/cancel" }

// ChatGPTCodexPath is "/settings/chatgpt/codex/path": PUT sets (field path) or clears (empty) the codex executable the
// user installed.
func ChatGPTCodexPath() string { return ChatGPTCodex() + "/path" }

// ChatGPTCodexRefresh is "/settings/chatgpt/codex/refresh": POST reads the account and the usage limit again.
func ChatGPTCodexRefresh() string { return ChatGPTCodex() + "/refresh" }

// ChatGPTCodexModels is "/settings/chatgpt/codex/models": GET lists the models of the signed-in account (model/list).
func ChatGPTCodexModels() string { return ChatGPTCodex() + "/models" }
