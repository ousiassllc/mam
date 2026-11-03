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

// Series are the groups the library installs: the series of Shisei and the label the screen shows. The suggestive series (sexy, duo) are not
// installed (see the asset set shisei-poses).
var Series = []struct{ ID, Label string }{{"std", "全身"}, {"upper", "上半身"}}

// Pose is a pose of the library as the installed catalog describes it: Views are the angles that have a picture on the disk (the value is the
// ID of the image, see Library.File).
type Pose struct {
	ID      string
	Series  string
	Class   string // the category, in Japanese
	Name    string // in Japanese
	NameEN  string
	ClassEN string
	Desc    string // an English description of the figure
	Views   map[Angle]string
}

// Image is the ID of the picture of the angle ("" when the pose has none).
func (p Pose) Image(a Angle) string { return p.Views[a] }

// FirstAngle is the angle to show a pose in: the wanted one when it has a picture, else the first it has.
func (p Pose) FirstAngle(want Angle) Angle {
	if p.Views[want] != "" {
		return want
	}
	for _, a := range Angles {
		if p.Views[a.Angle] != "" {
			return a.Angle
		}
	}
	return want
}

// imageID is the ID of a picture: <series>_<pose id>_<angle>, the file name of the picture without its extension.
var imageID = regexp.MustCompile(`^(std|upper)_[A-Z][0-9]{3}_(front|diag|side|above|below)$`)

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

// readIndex reads the catalog of the library (catalogFile in dir) and keeps the poses that have a picture below dir/poses, in the order of the
// catalog. A pose that is not rated general is never kept, whatever is on the disk.
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
		p := Pose{ID: e.ID, Series: e.Series, Class: e.ClassJP, Name: e.NameJP, NameEN: e.NameEN, ClassEN: e.ClassEN, Desc: e.Desc, Views: map[Angle]string{}}
		for _, a := range Angles {
			file := e.Views[string(a.Angle)]
			id, ok := strings.CutSuffix(file, ".webp")
			if !ok || !imageID.MatchString(id) {
				continue
			}
			if st, err := os.Stat(filepath.Join(dir, picturesDir, file)); err == nil && st.Mode().IsRegular() {
				p.Views[a.Angle] = id
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
