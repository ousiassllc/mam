package posescreen

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentestposes"
)

// filters are what the gallery is filtered by.
type filters struct {
	text, series, class string
	angle               poses.Angle
	kind                poses.Kind
}

// classValue is the value of an option of the select of categories: "<series>/" for a whole series, "<series>/<category>" for one.
func classValue(series, class string) string { return series + "/" + class }

// queryOf reads the filters of the request; what is not understood is "no filter" (the first angle is the front).
func queryOf(c *gin.Context) filters {
	f := filters{text: strings.TrimSpace(c.Query(gentestposes.FieldQuery)), angle: poses.Front, kind: poses.Line}
	if series, class, ok := strings.Cut(c.Query(gentestposes.FieldClass), "/"); ok {
		for _, s := range poses.Series {
			if s.ID == series {
				f.series, f.class = series, class
			}
		}
	}
	for _, a := range poses.Angles {
		if string(a.Angle) == c.Query(gentestposes.FieldAngle) {
			f.angle = a.Angle
		}
	}
	for _, k := range poses.Kinds {
		if string(k.Kind) == c.Query(gentestposes.FieldKind) {
			f.kind = k.Kind
		}
	}
	return f
}

func galleryOf(all []poses.Pose, f filters, notes gentestposes.Notes) gentestposes.Gallery {
	g := gentestposes.Gallery{Notes: notes, Query: f.text, All: gentestposes.Option{Value: "", Label: "すべて", Selected: f.series == ""}, Results: resultsOf(all, f)}
	for _, s := range poses.Series {
		classes := poses.Classes(all, s.ID)
		if len(classes) == 0 {
			continue
		}
		grp := gentestposes.ClassGroup{Label: s.Label, Options: []gentestposes.Option{
			{Value: classValue(s.ID, ""), Label: s.Label + "（すべて）", Selected: f.series == s.ID && f.class == ""},
		}}
		for _, cl := range classes {
			grp.Options = append(grp.Options, gentestposes.Option{Value: classValue(s.ID, cl), Label: cl, Selected: f.series == s.ID && f.class == cl})
		}
		g.Classes = append(g.Classes, grp)
	}
	for _, a := range poses.Angles {
		g.Angles = append(g.Angles, gentestposes.Option{Value: string(a.Angle), Label: a.Label, Selected: a.Angle == f.angle})
	}
	for _, k := range poses.Kinds {
		g.Kinds = append(g.Kinds, gentestposes.Option{Value: string(k.Kind), Label: k.Label, Selected: k.Kind == f.kind})
	}
	return g
}

func resultsOf(all []poses.Pose, f filters) gentestposes.Results {
	r := gentestposes.Results{Total: len(all)}
	for _, p := range poses.Filter(all, poses.Query{Text: f.text, Series: f.series, Class: f.class}) {
		angle := p.FirstAngleOf(f.kind, f.angle)
		pic := p.ImageOf(f.kind, angle) // a pose that has no map of this kind is not listed
		if pic == "" {
			continue
		}
		r.Cards = append(r.Cards, gentestposes.Card{
			Name: p.Name, NameEN: p.NameEN, Class: p.Class, Image: ImageURL(pic), Angle: angleLabel(angle), Value: ValuePrefix + pic, Other: angle != f.angle,
		})
	}
	return r
}

// cacheControl is the Cache-Control of a picture: an hour for a general one, none for a picture of a suggestive series, which the browser must not
// keep past the age gate (Issue #1020: the gate can be closed within the hour, and a cached picture would still show).
func cacheControl(pictureID string) string {
	if poses.IsAdultImage(pictureID) {
		return "no-store"
	}
	return "private, max-age=3600"
}
