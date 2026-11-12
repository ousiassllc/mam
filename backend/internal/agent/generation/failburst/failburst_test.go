package failburst_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/generation/failburst"
)

// The images of a pack that fail for one cause say it once, whatever kind of image each is (#1112); a different cause is kept.
func TestDistinct_DropsTheRepeatsOfOneCause(t *testing.T) {
	cause := errors.New("every image model failed for new_reference: gpt-image-2.6-flare: API key is not configured: openai")
	errs := []error{fmt.Errorf("character: %w", cause), fmt.Errorf("product: %w", cause), fmt.Errorf("location: %w", errors.New("rate limited")), fmt.Errorf("style: %w", cause), errors.New("no kind")}

	assert.Equal(t, []error{errs[0], errs[2], errs[4]}, failburst.Distinct(errs))
}

func TestBurst_TheFirstFailureOfACauseIsToldAndTheRestAreNamedAtTheEnd(t *testing.T) {
	var b failburst.Burst
	b.Enter("p")
	b.Enter("p")
	b.Enter("p")

	assert.True(t, b.First("p", "cause", "Shot 1"))
	assert.False(t, b.First("p", "cause", "Shot 2"))
	assert.True(t, b.First("p", "other", "Shot 3"), "another cause is told on its own")
	assert.Nil(t, b.Leave("p"))
	assert.Nil(t, b.Leave("p"))
	assert.Equal(t, map[string][]string{"cause": {"Shot 2"}, "other": nil}, b.Leave("p"), "the last job of the burst hands over what was not told")
	assert.True(t, b.First("p", "cause", "Shot 1"), "a failure after the burst is told again")
}
