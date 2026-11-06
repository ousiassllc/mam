package mmview

import (
	"errors"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// Failure is err as the common error (#1010), when the model manager refuses it: a missing consent is license_not_accepted, a missing engine
// engine_not_installed, a model that is not installed model_not_installed, no GPU no_gpu; the other refusals keep their sentence (Refuse)
// under the code of their kind (invalid_input, conflict, not_found). nil for an error that is not a refusal: the caller gives it to errmap.
// The error behind stays reachable, and its text is the detail.
func Failure(err error) *apperr.Error {
	var e *apperr.Error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, modelmanager.ErrConsentRequired):
		e = apperr.New(apperr.CodeLicense)
	case errors.Is(err, modelmanager.ErrEngineNotInstalled):
		e = apperr.New(apperr.CodeEngineMissing)
	case errors.Is(err, modelmanager.ErrNotInstalled):
		e = apperr.New(apperr.CodeModelMissing)
	case errors.Is(err, modelmanager.ErrNoGPU):
		e = apperr.New(apperr.CodeNoGPU).WithCause(guidance(err))
	default:
		var worded interface{ UserMessage() string }
		isWorded := errors.As(err, &worded)
		if known, ok := errmap.Known(err); ok && known.Code != apperr.CodeUnknown && (!isWorded || !vendorCode(known.Code)) {
			return known // a disk, a folder or a vendor failure: errmap words it
		}
		kind, msg := Refuse(err)
		switch {
		case kind == RefusalNone && isWorded:
			// an install that the installer worded itself: its sentence is the cause, and what it was worded from (the HTTP status, the file) the detail
			e := apperr.New(apperr.CodeInstallFailed).WithCause(worded.UserMessage()).Wrap(err)
			if behind := errors.Unwrap(err); behind != nil {
				e = e.WithDetail(behind.Error())
			}
			return e
		case kind == RefusalNone:
			return nil
		}
		switch kind {
		case RefusalInvalid:
			e = apperr.New(apperr.CodeInvalidInput)
		case RefusalNotFound:
			e = apperr.New(apperr.CodeNotFound)
		default:
			e = apperr.New(apperr.CodeConflict)
		}
		e.Cause = msg
	}
	return e.Wrap(err)
}

// vendorCode says a code is the answer of a service that mam called (a model vendor, ComfyUI): its words and remedy are about that call. An
// install that the installer worded itself (the source of a file is gone, the connection was lost) keeps its own sentence over them: the
// download of a file is not a call to a vendor, and "check the URL of the endpoint" is no remedy for it.
func vendorCode(c apperr.Code) bool {
	switch c {
	case apperr.CodeRemoteNotFound, apperr.CodeConnectionFailed, apperr.CodeTimeout, apperr.CodeServerError, apperr.CodeRequestRejected:
		return true
	}
	return false
}
