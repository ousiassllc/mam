package paths

// LLMLocal is "/settings/llm/local": PUT saves the card "ローカル LLM サーバー" of the category "テキスト生成（LLM）" (the base URL, the optional API key,
// the unload switch; fields base_url, api_key, clear_key, unload) and answers with the card.
func LLMLocal() string { return LLMSettings() + "/local" }

// LLMLocalTest is "/settings/llm/local/test": POST tries the server of the form (the typed base URL and key, else the saved ones) with
// GET /v1/models and answers with the result alone.
func LLMLocalTest() string { return LLMLocal() + "/test" }
