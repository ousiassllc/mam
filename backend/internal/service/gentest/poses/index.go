package poses

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Angle is one of the five camera angles a pose has a picture for.
type Angle string

// The angles of Shisei, in the order the gallery lists them.
const (
	Front Angle = "front"
	Diag  Angle = "diag"
	Side  Angle = "side"
	Above Angle = "above"
	Below Angle = "below"
)

// AngleChoice is an angle with the label the screen shows.
type AngleChoice struct {
	Angle Angle
	Label string
}

// Angles are the angles of a pose, with their labels.
var Angles = []AngleChoice{{Front, "正面"}, {Diag, "斜め"}, {Side, "横"}, {Above, "上から"}, {Below, "下から"}}

// Kind is which picture of a pose is meant: the line art, or the depth or normal map that Shisei made from it (the control image of a
// ControlNet; Issue #1020). The line art is what the library started with and is the zero choice everywhere.
type Kind string

// The kinds of picture a pose has. Depth and Normal are in the library's zips of the same names; only the angles Shisei made one for exist.
const (
	Line   Kind = "line"
	Depth  Kind = "depth"
	Normal Kind = "normal"
)

// KindChoice is a kind with the label the screen shows.
type KindChoice struct {
	Kind  Kind
	Label string
}

// Kinds are the kinds of picture, with their labels, in the order the gallery lists them.
var Kinds = []KindChoice{{Line, "線画"}, {Depth, "深度"}, {Normal, "法線"}}

// dir is the directory below the library's own where the pictures of the kind are (the dest of the members of the asset set).
func (k Kind) dir() string {
	if k == Line {
		return picturesDir
	}
	return string(k)
}

// Series are the groups the library installs: the series of Shisei and the label the screen shows. The suggestive series (sexy, duo) are not
// installed (see the asset set shisei-poses).
var Series = []struct{ ID, Label string }{{"std", "全身"}, {"upper", "上半身"}}

// Pose is a pose of the library as the installed catalog describes it: Views are the angles that have a line picture on the disk (the value is
// the ID of the image, see Library.File); Maps are the depth and normal maps of the angles that have one.
type Pose struct {
	ID      string
	Series  string
	Class   string // the category, in Japanese
	Name    string // in Japanese
	NameEN  string
	ClassEN string
	Desc    string // an English description of the figure
	Views   map[Angle]string
	Maps    map[Kind]map[Angle]string
}

// views are the pictures of the kind by angle.
func (p Pose) views(k Kind) map[Angle]string {
	if k == Line {
		return p.Views
	}
	return p.Maps[k]
}

// Image is the ID of the line picture of the angle ("" when the pose has none).
func (p Pose) Image(a Angle) string { return p.Views[a] }

// ImageOf is the ID of the picture of the kind at the angle ("" when the pose has none).
func (p Pose) ImageOf(k Kind, a Angle) string { return p.views(k)[a] }

// FirstAngle is the angle to show a pose in: the wanted one when it has a line picture, else the first it has.
func (p Pose) FirstAngle(want Angle) Angle { return p.FirstAngleOf(Line, want) }

// FirstAngleOf is FirstAngle for the pictures of the kind.
func (p Pose) FirstAngleOf(k Kind, want Angle) Angle {
	v := p.views(k)
	if v[want] != "" {
		return want
	}
	for _, a := range Angles {
		if v[a.Angle] != "" {
			return a.Angle
		}
	}
	return want
}

// imageID is the ID of a picture: <series>_<pose id>_<angle>, the file name of the picture without its extension, and ".depth" or ".normal"
// after it for the depth or normal map of the same pose and angle (no suffix: the line picture).
var imageID = regexp.MustCompile(`^(std|upper)_[A-Z][0-9]{3}_(front|diag|side|above|below)(\.(depth|normal))?$`)

// splitID cuts the ID of a picture into the file name without its extension and the kind (the ID must match imageID).
func splitID(id string) (name string, k Kind) {
	if name, ok := strings.CutSuffix(id, "."+string(Depth)); ok {
		return name, Depth
	}
	if name, ok := strings.CutSuffix(id, "."+string(Normal)); ok {
		return name, Normal
	}
	return id, Line
}

// pictureID is the ID of the picture of the kind that has the file name (without its extension).
func pictureID(name string, k Kind) string {
	if k == Line {
		return name
	}
	return name + "." + string(k)
}

// catalogEntry is a pose in data/catalog.json of the Shisei repository (the fields this reads).
type catalogEntry struct {
	ID      string            `json:"id"`
	Series  string            `json:"series"`
	Rating  string            `json:"rating"`
	ClassJP string            `json:"class_jp"`
	ClassEN string            `json:"class_en"`
	NameJP  string            `json:"name_jp"`
	NameEN  string            `json:"name_en"`
	Desc    string            `json:"desc_en"`
	Views   map[string]string `json:"views"`
}

// readIndex reads the catalog of the library (catalogFile in dir) and keeps the poses that have a line picture below dir/poses (and the depth
// and normal maps below dir/depth and dir/normal), in the order of the catalog. A pose that is not rated general is never kept, whatever is
// on the disk.
func readIndex(dir string) ([]Pose, error) {
	data, err := os.ReadFile(filepath.Join(dir, catalogFile)) //nolint:gosec // a fixed name below the library's own directory
	if err != nil {
		return nil, err
	}
	var entries []catalogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("the catalog of the pose library is not readable: %w", err)
	}
	var out []Pose
	for _, e := range entries {
		if e.Rating != "general" {
			continue
		}
		p := Pose{ID: e.ID, Series: e.Series, Class: e.ClassJP, Name: e.NameJP, NameEN: e.NameEN, ClassEN: e.ClassEN, Desc: e.Desc, Views: map[Angle]string{},
			Maps: map[Kind]map[Angle]string{Depth: {}, Normal: {}}}
		for _, a := range Angles {
			file := e.Views[string(a.Angle)]
			name, ok := strings.CutSuffix(file, ".webp")
			if !ok || !imageID.MatchString(name) {
				continue
			}
			for _, c := range Kinds {
				k := c.Kind
				if st, err := os.Stat(filepath.Join(dir, k.dir(), file)); err == nil && st.Mode().IsRegular() {
					p.views(k)[a.Angle] = pictureID(name, k)
				}
			}
		}
		if len(p.Views) > 0 {
			out = append(out, p)
		}
	}
	return out, nil
}

// Query is what the gallery filters by: Text is searched in the names, the category, the number and the description (all words must be
// found, upper and lower case alike); Series and Class are exact ("" for any).
type Query struct {
	Text   string
	Series string
	Class  string
}

func (q Query) matches(p Pose) bool {
	if q.Series != "" && p.Series != q.Series || q.Class != "" && p.Class != q.Class {
		return false
	}
	hay := strings.ToLower(strings.Join([]string{p.ID, p.Name, p.NameEN, p.Class, p.ClassEN, p.Desc}, " "))
	for _, w := range strings.Fields(strings.ToLower(q.Text)) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// Filter lists the poses that match, in the order of the catalog.
func Filter(all []Pose, q Query) []Pose {
	var out []Pose
	for _, p := range all {
		if q.matches(p) {
			out = append(out, p)
		}
	}
	return out
}

// Classes lists the categories of the poses (of the series when it is given), each once, in the order they first appear.
func Classes(all []Pose, series string) []string {
	var out []string
	for _, p := range all {
		if (series == "" || p.Series == series) && !slices.Contains(out, p.Class) {
			out = append(out, p.Class)
		}
	}
	return out
}
