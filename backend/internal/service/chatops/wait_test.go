package chatops_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// The wait text of the budget confirmation names the buttons of the card: the domain cannot import the label, so the check is made from the service that owns it.
func TestWait_BudgetConfirmNamesTheButtonsOfTheCard(t *testing.T) {
	next := mainline.Wait{Phase: mainline.PhaseBudgetConfirm, Shots: 1}.NextAction()
	assert.Contains(t, next, "「"+chatops.Yes+"」", "the words are the label of the card's yes button")
	assert.Contains(t, next, "「"+chatops.No+"」")
}
