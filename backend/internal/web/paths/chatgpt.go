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
