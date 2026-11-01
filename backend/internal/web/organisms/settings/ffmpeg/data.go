// Package ffmpeg is the "ffmpeg" category of the global settings screen (spec/components/settings-screen-categories.md §8):
// where ffmpeg and ffprobe are found and the two paths the user can set, which take priority over the search
// (spec/architecture/ffmpeg-lookup.md).
package ffmpeg

// PanelID is what a save replaces.
const PanelID = "ffmpeg-panel"

// The names of the two form fields.
const (
	FieldFFmpeg  = "ffmpeg"
	FieldFFprobe = "ffprobe"
)

// Tool is one of the two executables.
type Tool struct {
	// Label is how the screen calls it ("ffmpeg"); Field is the name of its input.
	Label, Field string
	// Configured is the path in the input: the saved one, or what was typed and refused.
	Configured string
	// Found is the file the lookup finds with the saved path ("" when there is none).
	Found string
	// Problem is why there is none: the saved path cannot be used, or it is in none of the places searched.
	Problem string
}

// Data is the category. Problem is why the settings file cannot be read (the rest is then not shown). Notice and Error are the
// result of the last save.
type Data struct {
	Problem string
	Notice  string
	Error   string
	FFmpeg  Tool
	FFprobe Tool
	// SearchDirs are the directories searched when a path is not set, in order.
	SearchDirs []string
	// Download is the panel of the download of ffmpeg; nil when the app has no such service.
	Download *Download
}

// Description is what the category says under its title.
const Description = "動画のプレビュー・サムネイル・部分採用・品質判定（Judge）は、ffmpeg と ffprobe を使います。ここでパスを指定するとそれを使い、空のときは、mam を起動した環境の PATH と、Mac では Homebrew の場所（/opt/homebrew/bin と /usr/local/bin）から探し、そのどこにも無ければ、mam でダウンロードして入れたものを使います（Windows と Apple Silicon の Mac で、下のボタンから入れられます）。保存したパスと、ダウンロードして入れたものは、mam の次の起動から使われます。"

// DownloadID is the id of the panel of the download, which every action of it replaces.
const DownloadID = "ffmpeg-download"

// DownloadInstall is the download in hand.
type DownloadInstall struct {
	Running bool
	Percent int
	Message string
	// Error is why the last install failed (shown until another one starts). Cancelled: the user stopped it.
	Error     string
	Cancelled bool
	// Succeeded: an install finished in this run of mam; mam looks for ffmpeg at its start, so it must be started again.
	Succeeded bool
}

// Download is the "download ffmpeg" panel: what mam can fetch for this machine, and the state of the copy it fetched earlier.
// It is nil-free: a machine that cannot download has Supported false and the panel says so (or is left out by the caller).
type Download struct {
	// Supported: ffmpeg can be downloaded here (Windows x64, macOS on Apple silicon).
	Supported bool
	// Both of the tools are found now (by the settings or the search). Managed: the one found is the copy mam downloaded.
	Found, Managed bool
	// Installed, Version, UpdateAvailable and Broken describe the downloaded copy; PinnedVersion is what this mam would download.
	Installed       bool
	Version         string
	PinnedVersion   string
	UpdateAvailable bool
	Broken          bool
	Dir             string
	// DownloadBytes and ExtractedBytes are the sizes of the download and of the two executables.
	DownloadBytes  int64
	ExtractedBytes int64
	// Build says what is downloaded: the build, its publisher, where the source of that build is, and its license.
	Build         string
	Publisher     string
	SourceURL     string
	SourceCodeURL string
	License       string
	LicenseURL    string
	Install       DownloadInstall
	// Notice and Error are the result of the last action (a refusal, a removal).
	Notice string
	Error  string
}

// Polling is whether the panel asks for itself again: while a download runs.
func (d Download) Polling() bool { return d.Install.Running }
