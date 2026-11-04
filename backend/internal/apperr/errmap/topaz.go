package errmap

import (
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/topaz"
	"github.com/ousiassllc/mam/backend/internal/apperr"
)

// topazFailure is a failure of the Topaz Video API (#1031): no API key is stored, or the client reported an errorCode or failed in its transfer of
// a file; nil for any other error.
// The code is the one in the status of a failed job, so it is told by what the job is (the cause the user can act on), not by a status. An
// answer of the API (te.Status set) is left to its status, except the maintenance, which only the code tells apart from any other 503.
func topazFailure(err error) *apperr.Error {
	if errors.Is(err, topaz.ErrNoAPIKey) {
		return apperr.New(apperr.CodeKeyMissing).With("service", "{{key_missing.topaz}}")
	}
	var te *topaz.Error
	if !errors.As(err, &te) {
		return nil
	}
	if te.Status != 0 {
		if te.Code == topaz.CodeServerMaintenance {
			return apperr.New(apperr.CodeTopazMaintenance)
		}
		return nil
	}
	switch te.Code {
	case topaz.CodeCreditDifference:
		return apperr.New(apperr.CodeTopazCreditDiff)
	case topaz.CodeTimeoutUpload, topaz.CodeTimeoutQueue, topaz.CodeTimeoutProcessing:
		return apperr.New(apperr.CodeTopazJobTimeout)
	case topaz.CodeInvalidInput:
		return apperr.New(apperr.CodeTopazSourceInvalid)
	case topaz.CodeInsufficient:
		return apperr.New(apperr.CodeCreditsInsufficient)
	case topaz.CodeUnauthorized, topaz.CodeForbidden: // the key was deleted or the plan lapsed while the job ran
		return apperr.New(apperr.CodeAuthFailed)
	case topaz.CodeTransferFailed:
		return apperr.New(apperr.CodeTopazTransferFailed)
	}
	return apperr.New(apperr.CodeTopazJobFailed)
}
