package modelsettings_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// The keys the settings screen hides while an aggregator is chosen are the vendors of the models the aggregators serve (Kling 3.0 and 4.0).
func TestOwnVendors_AreTheKeysOfTheModelsAnAggregatorServes(t *testing.T) {
	assert.Equal(t, []string{"kling", "kling4"}, modelsettings.OwnVendors())
}
