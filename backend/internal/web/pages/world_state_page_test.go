package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// An entity's edit form offers "保存" only while one of its fields can be edited: with every editable field
// LOCKed the inputs are disabled, so a save carries no body and the server answers 422 "更新する項目がありません"
// (#656). The page explains the LOCK instead.
func TestWorldStatePage_LeavesOutTheSaveButtonWhenEveryEditableFieldIsLocked(t *testing.T) {
	tests := []struct {
		name    string
		locked  []string
		blocked bool
	}{
		{"nothing locked", nil, false},
		{"one field left editable", []string{"time", "lighting_state"}, false},
		{"every editable field locked", []string{"time", "lighting_state", "weather"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			locks := domain.LockSet{}
			for _, f := range tc.locked {
				locks[domain.LockKey{Target: domain.TargetLocation, ID: "l1", Field: f}] = struct{}{}
			}
			var sb strings.Builder

			require.NoError(t, pages.WorldStatePage("tok", organisms.WorldStateData{
				Project: domain.Project{ID: "p"},
				World:   domain.WorldContext{Locations: []domain.WorldLocation{{ID: "l1", Name: "Cafe"}}},
				Locks:   locks,
			}).Render(context.Background(), &sb))

			assert.Equal(t, !tc.blocked, strings.Contains(sb.String(), ">保存<"), "save button")
			assert.Equal(t, tc.blocked, strings.Contains(sb.String(), "data-entity-edit-blocked"), "blocked notice")
		})
	}
}
