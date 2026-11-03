package apperr

// messagesEn are the English texts: the same keys as messagesJa (a test pins it).
var messagesEn = map[string]string{
	"unknown.cause":  "The cause is not known. This is a failure mam did not expect.",
	"unknown.remedy": "Try again. If it keeps failing, report it with the detail and the error ID; the log of mam has the same ID.",

	"key_missing.cause":  "No API key is set.",
	"key_missing.remedy": "Register the API key of the service in Settings > API keys and sign-in.",

	"llm_key_missing.cause":  "No API key is set for the LLM. {route}",
	"llm_key_missing.remedy": "Register {who} in Settings > API keys and sign-in. {{advice.chatgpt}} {restart}",

	"auth_failed.cause":  "The endpoint refused the credentials {http}. The API key is wrong or expired, or it has no right for this call.",
	"auth_failed.remedy": "Check the key in Settings > API keys and sign-in and enter it again. For an endpoint you registered yourself (a local ComfyUI, ...), check its authentication setting too.",

	"remote_not_found.cause":  "The endpoint has no such URL or model (HTTP 404).",
	"remote_not_found.remedy": "Check that the URL and the model name are right and that the model is installed. The bundled ComfyUI shows what is installed in the model manager.",

	"timeout.cause":  "The other side did not answer in time.",
	"timeout.remedy": "Wait a while and try again. A cloud model may keep generating on the other side and be billed.",

	"stalled.cause":  "The local generation showed no progress for {after} and was stopped.",
	"stalled.remedy": "Check that ComfyUI has not stopped. If a heavy model shows no progress for long, raise the timeout (the time without progress before stopping) of the endpoint in Settings.",

	"time_limit.cause":  "The local generation ran past the overall limit ({after}) and was stopped.",
	"time_limit.remedy": "Make the resolution or the duration smaller and run it again.",

	"endpoint_timeout.cause":  "The local generation ran past the waiting time of its endpoint ({after}) and was stopped.",
	"endpoint_timeout.remedy": "Raise the timeout of the endpoint in Settings, or make the resolution or the duration smaller and run it again.",

	"request_rejected.cause":  "The other side refused the request{http}. The input or a setting does not fit the model.",
	"request_rejected.remedy": "The detail has the reason the other side gave. Review the prompt, the resolution, the duration and the input images.",

	"input_missing.cause":  "An input the model always uses was not sent ({param}).",
	"input_missing.remedy": "Give the picture for {param} and run it again. A model that makes a video from a picture needs the first frame (first_frame).",

	"input_unsupported.cause":  "The model does not take this combination of input images or this resolution.",
	"input_unsupported.remedy": "Change how the input images are used (first frame, last frame, reference) or the resolution to what the model supports. The model list shows what each model takes.",

	"rate_limited.cause":  "The service's usage limit (rate limit, HTTP 429) was reached.",
	"rate_limited.remedy": "Wait a while and run it again. If it continues, run fewer calls at once or check the limit of your plan with the service.",

	"server_error.cause":  "The service has a fault on its side{http}.",
	"server_error.remedy": "Wait a while and try again. If it continues, check the status page of the service. For a local ComfyUI, check the log of ComfyUI.",

	"connection_failed.cause":  "The endpoint could not be reached.",
	"connection_failed.remedy": "Check that the endpoint is running, and the URL and the network (proxy, VPN, firewall). For the bundled ComfyUI, check in the model manager that the engine is running.",

	"circuit_open.cause":  "Calls to this model's vendor are paused because it kept failing.",
	"circuit_open.remedy": "Wait a while and run it again.",

	"vram_oom.cause":  "The GPU ran out of memory (VRAM).",
	"vram_oom.remedy": "Make the resolution or the duration smaller, use a lighter model or a faster mode (lightning, ...), or close other apps that use the GPU, then run it again.",

	"disk_full.cause":  "There is not enough free disk space{where}.",
	"disk_full.remedy": "Delete models you do not use or old results to free space, then run it again.",

	"model_not_installed.cause":  "A file of the model that is needed was not found.",
	"model_not_installed.remedy": "Install the model in the model manager. If it is installed already, install it again.",

	"license_not_accepted.cause":  "The license of this model is not accepted yet.",
	"license_not_accepted.remedy": "Read the license in the model manager and accept it before installing.",

	"engine_not_installed.cause":  "The ComfyUI engine is not installed.",
	"engine_not_installed.remedy": "Install the ComfyUI engine in the model manager first. To use an external ComfyUI, register its URL in Settings > local and alternative models.",

	"node_missing.cause":  "A custom node that the workflow uses is missing from ComfyUI.",
	"node_missing.remedy": "Install the custom node in the model manager, restart ComfyUI and run it again.",

	"llm_invalid_output.cause":  "The LLM's answer was not in the requested form.",
	"llm_invalid_output.remedy": "Run it again. If it continues, change the model of that Agent in Settings > text generation (LLM).",

	"not_writable.cause":  "The folder cannot be written.",
	"not_writable.remedy": "Move it to a place that can be written or check the permissions of the folder. Using the URL of an external ComfyUI is another way.",

	"no_gpu.cause":  "No supported GPU was found.",
	"no_gpu.remedy": "Set the URL of an external ComfyUI, or choose CPU execution explicitly.",

	"image_unavailable.cause":  "The ChatGPT account has no model that can host the image generation.",
	"image_unavailable.remedy": "Check the plan and the sign-in of ChatGPT and sign in again from Settings > ChatGPT sign-in.",

	"save_failed.cause":  "The setting or the data could not be saved.",
	"save_failed.remedy": "Check the free disk space and the permissions of the folder and save again. If it continues, check the log of mam.",

	"invalid_input.cause":    "The input is not valid.",
	"invalid_input.remedy":   "Correct the input and run it again.",
	"not_found.cause":        "The target was not found.",
	"not_found.remedy":       "Reload the screen or check the ID you gave.",
	"conflict.cause":         "This cannot be done in the present state.",
	"conflict.remedy":        "Wait until the running work ends, or check the state, and try again.",
	"locked.cause":           "A LOCKed item cannot be changed.",
	"locked.remedy":          "Release the LOCK and give the instruction again.",
	"forbidden.cause":        "This breaks a constraint such as the Brand / Safety Constraint.",
	"forbidden.remedy":       "Change the content so that it fits the constraints.",
	"budget_exceeded.cause":  "This cannot be run because it would exceed the budget ceiling.",
	"budget_exceeded.remedy": "Review the budget ceiling or reduce the number of items or the models.",
	"unavailable.cause":      "This feature is not available now (an API key or a setting is missing).",
	"unavailable.remedy":     "Check the API keys and the settings in Settings.",
	"upstream_failed.cause":  "The external model or service did not give a usable answer.",
	"upstream_failed.remedy": "Wait a while and try again.",

	"place.settings":      "Open Settings",
	"place.api-keys":      "Open Settings: API keys and sign-in",
	"place.llm":           "Open Settings: text generation (LLM)",
	"place.local-models":  "Open Settings: local and alternative models",
	"place.model-manager": "Open the model manager",
	"place.chatgpt":       "Open Settings: ChatGPT sign-in",

	"ui.title":    "It failed",
	"ui.cause":    "Cause",
	"ui.remedy":   "What to do",
	"ui.settings": "Related screens",
	"ui.detail":   "Show the detail",
	"ui.copy":     "Copy the detail",
	"ui.copied":   "Copied",
	"ui.error_id": "Error ID",

	"llm_key_missing.route": "The Agent that failed is {agent}. Providers and models tried: {tried}.",
	"llm_key_missing.named": "the {providers} API key",
	"llm_key_missing.any":   "the API key of the provider you use (Anthropic / OpenAI / Google for the LLM)",
	"advice.chatgpt":        "To run on a ChatGPT plan instead, sign in to ChatGPT in \"API keys and sign-in\" first, then set the provider of this Agent to chatgpt-subscription (chatgpt-codex for an organization account) in \"Text generation (LLM)\".",
	"restart.pending":       "Of the settings you saved, {items} take effect after a restart (\"Restart to apply\" in Settings).",
	"why.no_key":            " (no API key)",
	"why.circuit_open":      " (temporarily unavailable)",
	"why.failed":            " (the call failed)",

	"http.status": " (HTTP {status})",
	"where.path":  " (at {path})",
}
