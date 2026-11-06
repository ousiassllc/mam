package apperr

// messagesEn are the English texts: the same keys as messagesJa (a test pins it).
var messagesEn = map[string]string{ //nolint:gosec // texts of errors (they say "credits" and "API key", but hold no credential)
	"unknown.cause":  "The cause is not known. This is a failure mam did not expect.",
	"unknown.remedy": "Try again. If it keeps failing, report it with the detail and the error ID; the log of mam has the same ID.",

	"key_missing.cause":  "No {service}API key is set.",
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

	"request_rejected.cause":  "The other side refused the request{http}. It hit a content policy, or the input or a setting does not fit the model.",
	"request_rejected.remedy": "The detail has the reason the other side gave. Review the prompt, the resolution, the duration and the input images.",

	"input_missing.cause":  "An input the model always uses was not sent ({param}).",
	"input_missing.remedy": "Give the picture for {param}, or change the role of the picture you attached, and run it again. A model that makes a video from a picture needs the first frame (first_frame); if the picture is a \"reference\", change it to \"first frame\".",

	"input_unused.cause":  "The input images you attached are not used by the chosen models.",
	"input_unused.remedy": "To make something from the picture, choose an I2V (image to video) or a reference-image model. To make it without the picture, take the picture off and run it again.",

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

	"face_models_missing.cause":  "The face models for the face similarity are missing ({files}). The face similarity (SFace) is off.",
	"face_models_missing.remedy": "Install them with one button from the \"Face check models\" section of the model manager (/model-manager): they go to {dir} (YuNet is MIT, SFace is Apache-2.0; they come from a pinned commit of opencv/opencv_zoo and their SHA-256 is checked). You do not need to restart mam: the next generation test measures. To put them there by hand, copy them to {dir} (scripts/fetch-face-models.sh {dir} in the repository fetches the same pinned files). To use another folder, start mam with --face-model-dir. The face similarity also needs the ONNX Runtime library besides these models (the Windows installer puts it next to mam.exe).",

	"face_runtime_missing.cause":  "The ONNX Runtime library for the face similarity cannot be loaded. The face similarity (SFace) is off.",
	"face_runtime_missing.remedy": "The face models (YuNet and SFace) are in place; installing them again from the model manager does not fix this (the ONNX Runtime library does). Provide the ONNX Runtime 1.23.x library (on Windows onnxruntime.dll next to mam.exe: the installer version has put it there; the portable version does not include it, so open onnxruntime-win-x64-1.23.2.zip of v1.23.2 at https://github.com/microsoft/onnxruntime/releases and copy lib/onnxruntime.dll next to mam.exe, no command needed; libonnxruntime elsewhere). --onnxruntime-lib sets its location. If the DLL is there but does not load on Windows, the Microsoft Visual C++ 2015-2022 Redistributable (x64) is needed. Once it is in place the next generation test measures (no restart needed). If the detail shows that a face model file failed to load, remove them in the \"Face check models\" section of the model manager and install them again.",

	"llm_invalid_output.cause":  "The LLM's answer was not in the requested form.",
	"llm_invalid_output.remedy": "Run it again. If it continues, change the model of that Agent in Settings > text generation (LLM).",

	"local_llm_unreachable.cause":  "Cannot connect to the local server. The local LLM server (Ollama, LM Studio, llama.cpp server, ...) did not answer.",
	"local_llm_unreachable.remedy": "Check that the server is running and that the base URL under \"Local LLM server\" in Settings > text generation (LLM) is right (Ollama: http://localhost:11434, LM Studio: http://localhost:1234). LM Studio needs its local server started (Start Server).",

	"generation_time_limit.cause":  "The generation ran past the time limit (Max generation time).",
	"generation_time_limit.remedy": "Raise the time limit in Settings or review the Policy.",

	"media_timeout.cause":  "The video / audio processing (ffmpeg) ran past its time limit.",
	"media_timeout.remedy": "Check that the source file is not broken or too large and run it again.",

	"download_corrupt.cause":  "The downloaded file did not match its fixed SHA-256 or size and was discarded (the source may have been tampered with, or the transfer was corrupted).",
	"download_corrupt.remedy": "Install it again. If it continues, update mam and try again.",

	"install_failed.cause":  "The install could not be completed.",
	"install_failed.remedy": "The reason is in the detail. Install it again from the model manager.",

	"training_failed.cause":  "The voice training could not be completed.",
	"training_failed.remedy": "Close the other apps that use the GPU (such as a generation of the bundled ComfyUI) and train again. If it keeps failing, read the detail (the end of the training log) or check GPT-SoVITS in the model manager. Nothing of the unfinished run is kept.",

	"not_writable.cause":  "The folder cannot be written.",
	"not_writable.remedy": "Move it to a place that can be written or check the permissions of the folder. Using the URL of an external ComfyUI is another way.",

	"no_gpu.cause":  "No supported GPU was found.",
	"no_gpu.remedy": "Set the URL of an external ComfyUI, or choose CPU execution explicitly.",

	"image_unavailable.cause":  "No Codex model that can host the image generation was found for the ChatGPT subscription (OpenAI may have changed the models a ChatGPT sign-in can use).",
	"image_unavailable.remedy": "Update mam to the latest version and run it again. If it does not help, turn the ChatGPT image generation OFF in Settings and switch to an image model that works with an API key.",

	"save_failed.cause":  "The setting or the data could not be saved{why}.",
	"save_failed.remedy": "Check the free disk space and the permissions of the folder and save again. If it continues, check the log of mam.",

	"ffmpeg_missing.cause":  "ffmpeg and ffprobe were not found.",
	"ffmpeg_missing.remedy": "In Settings > ffmpeg, download ffmpeg and ffprobe, or give the path of the ones you installed yourself.",

	"media_failed.cause":  "{what} failed. The video / audio processing (ffmpeg) ended with an error.",
	"media_failed.remedy": "Check that the source video is not broken, and the free space and the write permission of the destination, then run it again. If it continues, report it with the detail (the end of the output of ffmpeg).",

	"facefusion_unavailable.cause":  "FaceFusion cannot be used. It was not found, or the path that is set cannot be used.",
	"facefusion_unavailable.remedy": "mam does not ship FaceFusion; install it yourself. If it is installed, check the paths of Python and the FaceFusion folder in Settings > FaceFusion. The detail has why it was not found and where mam looked.",

	"facefusion_failed.cause":  "FaceFusion could not make the picture. {why}",
	"facefusion_failed.remedy": "Check that a face is clearly visible in the face picture and in the target picture. FaceFusion refuses adult images. A shortage of GPU memory or a faulty install of FaceFusion can also be the cause. Check the detail (the end of the output of FaceFusion) and the state shown in Settings > FaceFusion.",

	"update_network.cause":  "The update check could not connect to the network.",
	"update_network.remedy": "Check the network connection. mam checks again by itself after a while.",

	"update_rate_limit.cause":  "The update could not be checked because the request limit of GitHub was reached.",
	"update_rate_limit.remedy": "Wait a while. mam checks again by itself.",

	"update_access.cause":  "The update release could not be fetched from GitHub (access was refused, or the file is gone).",
	"update_access.remedy": "Check again after a while. If it continues, check that a proxy or a firewall does not block GitHub.",

	"update_verification.cause":  "The downloaded update file could not be verified (it may be damaged or tampered with, so mam does not update).",
	"update_verification.remedy": "Check again after a while. If it continues, get a new version from the distributor and install it again.",

	"update_release.cause":  "The content of the published release is not valid.",
	"update_release.remedy": "Check again after a while. If it continues, tell the distributor.",

	"update_failed.cause":  "The update check failed for a reason that is not known.",
	"update_failed.remedy": "Check again. If it continues, read the log file.",

	"credits_insufficient.cause":  "The account of the service has too few credits (HTTP 402). The credits this job needs could not be reserved.",
	"credits_insufficient.remedy": "Add credits to the account of the service, or turn on auto top-up, and run it again. It fails the same way until the balance grows.",

	"topaz_maintenance.cause":  "The cloud video rendering of Topaz is switched off for maintenance (HTTP 503). A job that was running is refunded.",
	"topaz_maintenance.remedy": "Wait a while, then get a new quote and run it again. GET /video/status of Topaz tells whether it is available (isAvailable and a message).",

	"topaz_credit_difference.cause":  "Topaz examined the uploaded video again and the credits it needs differ a lot from the quote, so it stopped the job (it is refunded in full).",
	"topaz_credit_difference.remedy": "The duration, the frame count, the frame rate or the size sent may not match the real file. Check that ffmpeg (ffprobe) works in the settings, then get a new quote and run it again.",

	"topaz_job_timeout.cause":  "The Topaz job timed out (waiting for the upload, waiting in the queue or the processing time). The credits are refunded.",
	"topaz_job_timeout.remedy": "Get a new quote and run it again. Try a shorter video for a long one.",

	"topaz_job_failed.cause":  "Topaz could not finish the job (a failure on its side, or the job was cancelled). The credits are refunded.",
	"topaz_job_failed.remedy": "Get a new quote and run it again. If it continues with the same video, try the video converted to a standard H.264 mp4, or send the request ID in the detail to the Topaz support. mam never repeats the job by itself (it would be charged twice).",

	"topaz_source_invalid.cause":  "Topaz could not process the source video (it may be damaged, in another format than its extension says, or its facts differ from the real ones). Nothing is charged.",
	"topaz_source_invalid.remedy": "Check that the source video plays, then run it again. If it continues, convert the video to a standard H.264 mp4 and try again.",

	"topaz_transfer_failed.cause":  "The transfer of the video to or from Topaz failed (the upload of the source or the download of the result).",
	"topaz_transfer_failed.remedy": "Check the network (proxy, VPN, firewall). If the upload failed, get a new quote and run it again. A failed download of a finished job can be tried later: Topaz keeps the result for 7 days. mam never repeats the job by itself (it would be charged twice).",

	"topaz_job_interrupted.cause":  "mam stopped, or the job was cancelled, while it waited for Topaz, so it stopped waiting for the result. Topaz had already accepted the job, so it may keep running there and the credits may be charged (request ID: {request_id}).",
	"topaz_job_interrupted.remedy": "mam cannot cancel a job at Topaz (the cancel call is not implemented). Look the request ID ({request_id}) up in your Topaz account to see the state of the job and the credits used. mam never repeats the job by itself (it would be charged twice).",

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

	"save_failed.disk":       " (there is not enough free disk space)",
	"save_failed.permission": " (the destination folder cannot be written)",

	"duration.hours":   "{n} h",
	"duration.minutes": "{n} min",
	"duration.seconds": "{n} s",

	"key_missing.elevenlabs": "ElevenLabs ",
	"key_missing.topaz":      "Topaz Labs ",

	"what.rough_cut": "Rendering the Rough Cut",
	"what.export":    "Exporting the video",
	"what.compose":   "Composing the Take",
	"what.audio_mix": "Mixing the audio",

	"facefusion.timeout":  "It did not finish in the time allowed and was stopped.",
	"facefusion.exit":     "FaceFusion ended with an error.",
	"facefusion.nooutput": "FaceFusion finished, but there is no output image.",

	"agent.orchestrator":        "Orchestrator (progress control)",
	"agent.producer":            "Producer (concept)",
	"agent.writer":              "Writer (script)",
	"agent.director":            "Director (Scene / Shot plan)",
	"agent.storyboard":          "Storyboard",
	"agent.art_director":        "Art Director (Reference Pack)",
	"agent.prompt_planner":      "Prompt Planner (Shot Spec)",
	"agent.video_judge":         "AI Video Judge (video evaluation)",
	"agent.repair":              "Repair (choice of the repair strategy)",
	"agent.editor":              "Editor (editing verdict)",
	"agent.audio_director":      "Audio Director (audio design)",
	"agent.command_interpreter": "Command Interpreter (DIRECTOR instructions)",
	"agent.state_extractor":     "State Extractor (World State extraction)",
	"agent.final_qc":            "Final QC (final inspection)",

	"list.separator": ", ",
	"list.join":      " / ",
}
