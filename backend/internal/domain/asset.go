package domain

import "time"

// AssetType is the asset.type column.
type AssetType string

// Asset types. Uploaded images and videos keep their media type; they are
// taken into the Brief as Assets (images) or References (videos).
const (
	AssetImage     AssetType = "image"
	AssetVideo     AssetType = "video"
	AssetReference AssetType = "reference"
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
