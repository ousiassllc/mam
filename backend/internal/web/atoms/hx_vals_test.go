package atoms_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

func TestHxVals_EncodesAJSONObjectEvenForEmptyAndSpecialValues(t *testing.T) {
	assert.Equal(t, `{"branch_id":""}`, atoms.HxVals(map[string]string{"branch_id": ""}))
	assert.Equal(t, `{"selected":"true"}`, atoms.HxVals(map[string]string{"selected": "true"}))
	assert.Equal(t, `{"content":"a \"b\" \u003cc\u003e"}`, atoms.HxVals(map[string]string{"content": `a "b" <c>`}))
	assert.Equal(t, `{}`, atoms.HxVals(map[string]string{}))
	assert.Equal(t, `{}`, atoms.HxVals(nil), "never null: htmx cannot read it and the request is not sent")
}
