package pages_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/seriesview"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// The forms that create something and then send the browser to its page show nothing until the server
// answers, so a second click or Enter would queue a second request (htmx's default) and make a second
// Project / Episode / Branch (#651), a second Series (#676), or the upload of the same bundle or Review
// Package twice (#676). Each drops a request while one is in flight and disables its submit
// button, like the chat composer and the "続きを作る" button of the Shot card.
func TestCreateAndRedirectForms_IgnoreASecondSubmitWhileOneIsInFlight(t *testing.T) {
	project := domain.Project{ID: "p", Name: "P"}
	tests := []struct {
		name   string
		page   templ.Component
		action string
	}{
		{"new project", pages.ProjectsPage("tok", nil, organisms.NewProjectFormData{}), `hx-post="/projects"`},
		{"next episode", pages.SeriesPage("tok", seriesview.Data{Series: domain.Series{ID: "s1", Name: "S"}}), `hx-post="/series/s1/episodes"`},
		{"branch", pages.BranchPage("tok", organisms.BranchPageData{Project: project, Shots: []organisms.BranchShotOption{{ShotID: "sh1", Label: "Scene 1 / Shot 1"}}}), `hx-post="/shots/sh1/branch"`},
		{"make a series of a project", organisms.SeriesMembership(project), `hx-post="/series"`},
		{"import a project bundle", pages.ProjectsPage("tok", nil, organisms.NewProjectFormData{}), `hx-post="/projects/import"`},
		{"import a review package", pages.ReviewInboxPage("tok", organisms.ReviewInboxData{}), `hx-post="/reviews/import"`},
	}
	formTag := regexp.MustCompile(`<form[^>]*>`)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var sb strings.Builder
			require.NoError(t, tc.page.Render(context.Background(), &sb))

			var form string
			for _, tag := range formTag.FindAllString(sb.String(), -1) {
				if strings.Contains(tag, tc.action) {
					form = tag
				}
			}
			require.NotEmpty(t, form, "the form with %s is rendered", tc.action)
			assert.Contains(t, form, `hx-sync="this:drop"`)
			assert.Contains(t, form, `hx-disabled-elt="find button[type=submit]"`)
		})
	}
}
