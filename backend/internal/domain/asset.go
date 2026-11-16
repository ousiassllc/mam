package domain

import "time"

// AssetType is the asset.type column.
type AssetType string

// Asset types. Images and videos are taken into the Brief as Assets (images) or References (videos); an audio file (WAV, MP3, FLAC, OGG, M4A) is no part of it: it is the sound of a speech-to-video generation (#1153).
const (
	AssetImage     AssetType = "image"
	AssetVideo     AssetType = "video"
	AssetReference AssetType = "reference"
	AssetAudio     AssetType = "audio"
)

// Asset is a file owned by a Project. FilePath is relative to the assets directory.
type Asset struct {
	ID        string
	ProjectID string
	Type      AssetType
	FilePath  string
	MimeType  string
	SizeBytes int64
	CreatedAt time.Time
}
