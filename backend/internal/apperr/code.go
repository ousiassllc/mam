// Package apperr is the one shape of a failure that a person reads (#1010): a code (the kind of failure), the cause and the remedy in
// words (from a catalog with a Japanese and an English text per code), the settings screens that help, and the detail the machine
// gave (the original message, a node number, an id). The screens (templ component molecules/errorbox), the MCP tools (ToolError,
// get_job, get_generation_result) and the history of a result all draw from it, so that one failure reads the same everywhere.
//
// It knows nothing of the layers above: the translation of the errors of ComfyUI, the vendors and the disk into a Code is
// package errmap. This package holds the types, the catalog and the lookup of the text.
package apperr

import (
	"maps"
	"slices"
)

// Code is the kind of a failure, the stable name a screen test or an agent branches on ("vram_oom", "input_missing").
type Code string

// The codes. Every code has a Japanese and an English text in the catalog (messages_ja.go, messages_en.go) and a class and the
// settings places in specs below; TestEveryCodeHasItsTexts pins that.
const (
	CodeUnknown          Code = "unknown"               // a failure none of the others fits: the cause is not known, the detail and the log have it
	CodeKeyMissing       Code = "key_missing"           // no API key is stored for the service
	CodeLLMKeyMissing    Code = "llm_key_missing"       // no API key for any provider an Agent of the Reasoning LLM may use
	CodeAuthFailed       Code = "auth_failed"           // 401 / 403
	CodeRemoteNotFound   Code = "remote_not_found"      // 404 from the service or the endpoint
	CodeTimeout          Code = "timeout"               // 408, or no answer within the time (a cloud call)
	CodeStalled          Code = "stalled"               // a local job showed no progress for the endpoint's timeout
	CodeTimeLimit        Code = "time_limit"            // a local call ran past the ceiling of a ComfyUI call
	CodeEndpointTimeout  Code = "endpoint_timeout"      // a local call ran past the timeout of its endpoint
	CodeRequestRejected  Code = "request_rejected"      // 4xx (422 and others): the request itself was refused
	CodeInputMissing     Code = "input_missing"         // an input the model needs was not sent ("needs first_frame")
	CodeInputUnsupported Code = "input_unsupported"     // an input image or resolution the model does not take
	CodeInputUnused      Code = "input_unused"          // every input image attached is one the chosen models cannot use: stopped before the call (#1009)
	CodeRateLimited      Code = "rate_limited"          // 429
	CodeServerError      Code = "server_error"          // 5xx
	CodeConnectionFailed Code = "connection_failed"     // the endpoint could not be reached
	CodeCircuitOpen      Code = "circuit_open"          // calls to the vendor are paused after repeated failures
	CodeVRAM             Code = "vram_oom"              // the GPU ran out of memory (CUDA OOM)
	CodeDiskFull         Code = "disk_full"             // no free disk space
	CodeModelMissing     Code = "model_not_installed"   // the model (a file of it) is not installed
	CodeLicense          Code = "license_not_accepted"  // the license of the model was not accepted
	CodeEngineMissing    Code = "engine_not_installed"  // the bundled ComfyUI engine is not installed
	CodeNodeMissing      Code = "node_missing"          // a custom node of the workflow is missing from ComfyUI
	CodeLLMInvalidOutput Code = "llm_invalid_output"    // the LLM answered something that is not the requested form
	CodeLocalLLMDown     Code = "local_llm_unreachable" // the local OpenAI-compatible LLM server (Ollama, LM Studio, llama.cpp server) did not answer
	CodeGenTimeLimit     Code = "generation_time_limit" // the Max generation time of the Policy ran out
	CodeMediaTimeout     Code = "media_timeout"         // ffmpeg / ffprobe ran past its limit
	CodeDownloadCorrupt  Code = "download_corrupt"      // the downloaded file does not match its checksum or size
	CodeInstallFailed    Code = "install_failed"        // an install of a model, a node or a runtime stopped for a reason the installer worded
	CodeNotWritable      Code = "not_writable"          // the folder cannot be written
	CodeNoGPU            Code = "no_gpu"                // no supported GPU
	CodeFaceModels       Code = "face_models_missing"   // the face models (YuNet, SFace) of the face similarity are not in the model folder (#1026)
	CodeFaceRuntime      Code = "face_runtime_missing"  // the ONNX Runtime library of the face similarity is missing or does not load (#1026)
	CodeImageUnavailable Code = "image_unavailable"     // the ChatGPT account hosts no model for the image generation
	CodeSaveFailed       Code = "save_failed"           // a setting or a file could not be saved
	CodeTrainFailed      Code = "training_failed"       // the fine-tuning of a Character's voice stopped for a reason the trainer worded (#936)

	// The codes of the classes below are for a failure that a service already worded (Error.Cause): the screen and the tool keep that
	// sentence and add the generic remedy.
	CodeInvalidInput Code = "invalid_input"
	CodeNotFound     Code = "not_found"
	CodeConflict     Code = "conflict"
	CodeLocked       Code = "locked"
	CodeForbidden    Code = "forbidden"
	CodeBudget       Code = "budget_exceeded"
	CodeUnavailable  Code = "unavailable"
	CodeUpstream     Code = "upstream_failed"
)

// The classes: the coarse sort of a failure, which is the code a tool error always had (mcpkit.Code*; they are these strings). A
// Code has exactly one class. They are untyped string constants so that mcpkit can use them as its own.
const (
	ClassValidation  = "validation"
	ClassNotFound    = "not_found"
	ClassConflict    = "conflict"
	ClassLocked      = "locked"
	ClassForbidden   = "forbidden"
	ClassBudget      = "budget_exceeded"
	ClassRateLimited = "rate_limited"
	ClassUnavailable = "unavailable"
	ClassUpstream    = "upstream"
	ClassInternal    = "internal"
)

// Place is a screen that helps with a failure: the settings category or the page the person goes to.
type Place string

// The places. Path and the label are in places below.
const (
	PlaceSettings    Place = "settings"
	PlaceAPIKeys     Place = "api-keys"
	PlaceLLM         Place = "llm"
	PlaceLocalModels Place = "local-models"
	PlaceModelMgr    Place = "model-manager"
	PlaceChatGPT     Place = "chatgpt"
)

// places is where each Place is (the paths of web/paths: Settings, SettingsCategory, ModelManager, ChatGPTSettings; a test pins it).
var places = map[Place]string{ //nolint:gosec // G101 false positive: the path of the API keys screen, not a credential
	PlaceSettings:    "/settings",
	PlaceAPIKeys:     "/settings/api-keys",
	PlaceLLM:         "/settings/llm",
	PlaceLocalModels: "/settings/local-models",
	PlaceModelMgr:    "/model-manager",
	PlaceChatGPT:     "/settings/chatgpt",
}

// spec is what a Code is: its class and the places that help.
type spec struct {
	class  string
	places []Place
}

var specs = map[Code]spec{
	CodeUnknown:          {ClassInternal, nil},
	CodeKeyMissing:       {ClassUnavailable, []Place{PlaceAPIKeys}},
	CodeLLMKeyMissing:    {ClassUnavailable, []Place{PlaceAPIKeys, PlaceLLM}},
	CodeAuthFailed:       {ClassUpstream, []Place{PlaceAPIKeys}},
	CodeRemoteNotFound:   {ClassUpstream, []Place{PlaceLocalModels, PlaceModelMgr}},
	CodeTimeout:          {ClassUpstream, nil},
	CodeStalled:          {ClassUpstream, []Place{PlaceLocalModels}},
	CodeTimeLimit:        {ClassUpstream, nil},
	CodeEndpointTimeout:  {ClassUpstream, []Place{PlaceLocalModels}},
	CodeRequestRejected:  {ClassUpstream, nil},
	CodeInputMissing:     {ClassValidation, nil},
	CodeInputUnsupported: {ClassValidation, nil},
	CodeInputUnused:      {ClassValidation, nil},
	CodeRateLimited:      {ClassRateLimited, nil},
	CodeServerError:      {ClassUpstream, nil},
	CodeConnectionFailed: {ClassUpstream, []Place{PlaceModelMgr, PlaceLocalModels}},
	CodeCircuitOpen:      {ClassUpstream, nil},
	CodeVRAM:             {ClassUpstream, nil},
	CodeDiskFull:         {ClassUnavailable, []Place{PlaceModelMgr}},
	CodeModelMissing:     {ClassUnavailable, []Place{PlaceModelMgr}},
	CodeLicense:          {ClassConflict, []Place{PlaceModelMgr}},
	CodeEngineMissing:    {ClassUnavailable, []Place{PlaceModelMgr, PlaceLocalModels}},
	CodeNodeMissing:      {ClassUnavailable, []Place{PlaceModelMgr}},
	CodeLLMInvalidOutput: {ClassUpstream, []Place{PlaceLLM}},
	CodeLocalLLMDown:     {ClassUpstream, []Place{PlaceLLM}},
	CodeGenTimeLimit:     {ClassUpstream, []Place{PlaceSettings}},
	CodeMediaTimeout:     {ClassUpstream, nil},
	CodeDownloadCorrupt:  {ClassUpstream, []Place{PlaceModelMgr}},
	CodeInstallFailed:    {ClassUpstream, []Place{PlaceModelMgr}},
	CodeTrainFailed:      {ClassUpstream, []Place{PlaceModelMgr}},
	CodeNotWritable:      {ClassUnavailable, []Place{PlaceModelMgr}},
	CodeNoGPU:            {ClassUnavailable, []Place{PlaceModelMgr, PlaceLocalModels}},
	CodeFaceModels:       {ClassUnavailable, nil},
	CodeFaceRuntime:      {ClassUnavailable, nil},
	CodeImageUnavailable: {ClassUnavailable, []Place{PlaceChatGPT}},
	CodeSaveFailed:       {ClassInternal, nil},
	CodeInvalidInput:     {ClassValidation, nil},
	CodeNotFound:         {ClassNotFound, nil},
	CodeConflict:         {ClassConflict, nil},
	CodeLocked:           {ClassLocked, nil},
	CodeForbidden:        {ClassForbidden, nil},
	CodeBudget:           {ClassBudget, nil},
	CodeUnavailable:      {ClassUnavailable, []Place{PlaceSettings}},
	CodeUpstream:         {ClassUpstream, nil},
}

// Codes returns every code, sorted by name.
func Codes() []Code { return slices.Sorted(maps.Keys(specs)) }
