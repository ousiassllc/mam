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
}

// Description is what the category says under its title.
const Description = "動画のプレビュー・サムネイル・部分採用・品質判定（Judge）は、ffmpeg と ffprobe を使います。ここでパスを指定するとそれを使い、空のときは、mam を起動した環境の PATH と、Mac では Homebrew の場所（/opt/homebrew/bin と /usr/local/bin）から探します。保存したパスは、mam の次の起動から使われます。"
