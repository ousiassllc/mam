package domain

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxSeriesNameRunes bounds the name of a Series; MaxStyleRunes bounds its Style text.
const (
	MaxSeriesNameRunes = 100
	MaxStyleRunes      = 500
)

// Series groups the Episodes (Projects) of one production and holds the canon they inherit: the
// Characters, Products and Locations, and the Style (F-29, spec/architecture/er.md §4).
type Series struct {
	ID   string
	Name string
	// Style is the canonical look-and-feel as text (copied into the Brief of each Episode);
	// StyleReferenceAssetID is its reference image ("" = none), copied into the Reference Library
	// of each Episode as a style reference.
	Style                 string
	StyleReferenceAssetID string
	// Version is the number of the latest series_version, the canon that new Episodes inherit.
	Version   int
	CreatedAt time.Time
}

// SeriesCanon is the canon of a Series at one version: series_version.state, and what the Series
// page shows. Characters, Products and Locations carry the canonical fields only (see the
// Canon* functions); the state of a story lives in the Episodes.
type SeriesCanon struct {
	Name                  string           `json:"name"`
	Style                 string           `json:"style,omitempty"`
	StyleReferenceAssetID string           `json:"style_reference,omitempty"`
	Characters            []WorldCharacter `json:"characters"`
	Products              []WorldProduct   `json:"products"`
	Locations             []WorldLocation  `json:"locations"`
}

// SeriesVersion is one entry of the history of the canon: every change appends a full snapshot.
type SeriesVersion struct {
	Number    int
	CreatedAt time.Time
}

// SeriesCharacterFields are the fields of a canonical Character that PUT /series/:id/characters/:id
// edits. The state a Shot changes (emotion, position, current_action) is not canon.
func SeriesCharacterFields() []string {
	return []string{"hairstyle", "wardrobe", "voice", "personality"}
}

// IsSeriesCharacterField reports whether name is one of SeriesCharacterFields.
func IsSeriesCharacterField(name string) bool { return slices.Contains(SeriesCharacterFields(), name) }

// CanonCharacter is what of a Character the canon keeps: identity, look and voice, without the
// emotion, position and action that the latest Shot left.
func CanonCharacter(c WorldCharacter) WorldCharacter {
	c.Emotion, c.Position, c.CurrentAction = "", "", ""
	return c
}

// CanonProduct is what of a Product the canon keeps: without its orientation in the last Shot.
func CanonProduct(p WorldProduct) WorldProduct {
	p.Orientation = ""
	return p
}

// CanonLocation is what of a Location the canon keeps: its layout, without the time of day,
// lighting and weather of the last Scene.
func CanonLocation(l WorldLocation) WorldLocation {
	l.Time, l.LightingState, l.Weather = "", "", ""
	return l
}

// ValidateSeriesName trims the name and checks it is 1..MaxSeriesNameRunes runes of one line.
func ValidateSeriesName(name string) (string, bool) {
	name = strings.Join(strings.Fields(name), " ")
	n := utf8.RuneCountInString(name)
	return name, n >= 1 && n <= MaxSeriesNameRunes
}

// EpisodeName is the display name of an Episode that has no Brief Subject of its own.
func EpisodeName(series string, number int) string {
	return series + " 第" + strconv.Itoa(number) + "話"
}
