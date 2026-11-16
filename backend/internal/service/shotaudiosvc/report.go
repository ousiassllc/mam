package shotaudiosvc

import "time"

// Report is the JSON of the audio of a Shot: what the MCP tools (get_shot_audio, set_shot_audio) and the JSON API (GET and PUT /api/v1/shots/{id}/audio) answer. The tags
// are those of both schemas (jsonschema for the MCP SDK, doc for Huma), so that the two entry points say the same thing.
type Report struct {
	ShotID    string         `json:"shot_id"`
	ProjectID string         `json:"project_id"`
	Audio     *ReportAudio   `json:"audio" jsonschema:"結び付けた音声。無ければ null" doc:"The audio the Shot is made from; null when it has none"`
	Choices   []ReportChoice `json:"choices" jsonschema:"結び付けられる Project の音声アセット（新しい順）" doc:"The audio assets of the Project the Shot can be tied to, newest first"`
	Locked    bool           `json:"locked" jsonschema:"Shot の Prompt が LOCK で、音声を変えられない" doc:"true when the Prompt of the Shot is LOCKed: the audio cannot be changed"`
	Busy      bool           `json:"busy" jsonschema:"Shot が生成中か修復中で、音声を変えられない" doc:"true while the Shot is being generated or repaired: the audio cannot be changed"`
}

// ReportAudio is the audio a Shot is tied to.
type ReportAudio struct {
	AssetID   string  `json:"asset_id"`
	MimeType  string  `json:"mime_type,omitempty"`
	SizeBytes int64   `json:"size_bytes,omitempty"`
	Seconds   float64 `json:"seconds,omitempty" jsonschema:"音声の長さ（秒）。読めないときは省略" doc:"Length of the sound in seconds; omitted when it cannot be read"`
	Usable    bool    `json:"usable" jsonschema:"いま生成に使えるか。false のとき problem に理由（アセットが無い・音声でない・読めない・上限の長さを超える）" doc:"Whether a generation can use it now; when false, problem says why"`
	Problem   string  `json:"problem,omitempty"`
}

// ReportChoice is an audio asset of the Project.
type ReportChoice struct {
	AssetID   string `json:"asset_id"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt string `json:"created_at"`
}

// Report is the View as the JSON entry points answer it.
func (v View) Report() Report {
	out := Report{ShotID: v.ShotID, ProjectID: v.ProjectID, Locked: v.Locked, Busy: v.Busy, Choices: make([]ReportChoice, 0, len(v.Choices))}
	if t := v.Tie; t != nil {
		out.Audio = &ReportAudio{AssetID: t.Asset.ID, MimeType: t.Asset.MimeType, SizeBytes: t.Asset.SizeBytes, Seconds: t.Seconds, Usable: t.Problem == "", Problem: t.Problem}
	}
	for _, a := range v.Choices {
		out.Choices = append(out.Choices, ReportChoice{AssetID: a.ID, MimeType: a.MimeType, SizeBytes: a.SizeBytes, CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339)})
	}
	return out
}
