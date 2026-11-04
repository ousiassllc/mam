package errmap

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/facefusion"
)

// faceFusion is the failures of the external tool FaceFusion (spec/architecture/gentest/face-swap.md): none installed, or a run that ended
// without a picture. It is tried after the messages (a run whose output says "out of memory" is a GPU failure whatever FaceFusion
// did) and before the status and the kinds (a run that hit its time limit wraps context.DeadlineExceeded, which is not a vendor's timeout).
func faceFusion(err error) *apperr.Error {
	var (
		cfg *facefusion.ConfigError
		run *facefusion.RunError
	)
	switch {
	case errors.As(err, &cfg), errors.Is(err, facefusion.ErrNotFound):
		return apperr.New(apperr.CodeFaceFusionNone)
	case errors.As(err, &run):
		why := "{{facefusion.exit}}"
		switch {
		case errors.Is(run.Err, context.DeadlineExceeded):
			why = "{{facefusion.timeout}}"
		case run.Err == nil:
			why = "{{facefusion.nooutput}}"
		}
		return apperr.New(apperr.CodeFaceFusionFailed).With("why", why)
	}
	return nil
}

// SaveFailure is the failure of a setting (or any file of the app's own) that could not be written: save_failed, with the disk or the permission
// as the reason when errmap knows it, and the failure as the detail. A failure the app already worded (an *apperr.Error) is returned as it is. The
// screens of the settings and the tools that save a setting both use it, so that a failed save reads the same on both. (The model manager's own
// codes for a full disk and a folder that cannot be written point at the models, and their remedies are about models, so they are not used.)
// nil for nil.
func SaveFailure(err error) *apperr.Error {
	if err == nil {
		return nil
	}
	if e, ok := apperr.As(err); ok {
		return e
	}
	e := apperr.New(apperr.CodeSaveFailed).WithDetail(detailOf(err)).Wrap(err)
	if known, ok := Known(err); ok {
		switch known.Code {
		case apperr.CodeDiskFull:
			_ = e.With("why", "{{save_failed.disk}}")
		case apperr.CodeNotWritable:
			_ = e.With("why", "{{save_failed.permission}}")
		}
	}
	return e
}

// machineFailure are the codes that say something of the machine, not of a model: they hold for ffmpeg too (the message ComfyUI's GPU failures are
// known by, "out of memory", is not read for ffmpeg: it says it of its own memory).
var machineFailure = map[apperr.Code]bool{
	apperr.CodeFFmpegMissing: true, apperr.CodeMediaTimeout: true, apperr.CodeDiskFull: true, apperr.CodeNotWritable: true,
}

// Media is the failure of a run of ffmpeg (the Rough Cut, the export, composing a Take, the audio mix) as the common error. what is the key of the
// work in the catalog ("what.export", ...). What errmap knows of the machine (no ffmpeg, the time limit, a full disk, a folder that cannot be
// written) is told as it is; any other is media_failed, whose detail is what ffmpeg said. nil for nil.
func Media(what string, err error) *apperr.Error {
	if err == nil {
		return nil
	}
	if e, ok := Known(err); ok && machineFailure[e.Code] {
		return e
	}
	return apperr.New(apperr.CodeMediaFailed).With("what", "{{"+what+"}}").WithDetail(detailOf(err)).Wrap(err)
}

// updateCodes is the code of each kind of failed update check (the ErrorKind of service/updater: the strings are its values; the packages
// above the services use them without importing the updater).
var updateCodes = map[string]apperr.Code{
	"network":      apperr.CodeUpdateNetwork,
	"rate_limit":   apperr.CodeUpdateRateLimit,
	"access":       apperr.CodeUpdateAccess,
	"verification": apperr.CodeUpdateVerify,
	"release":      apperr.CodeUpdateRelease,
}

// UpdateCheck is the failure of an update check as the common error, by the kind the updater classified it as ("network", "rate_limit",
// "access", "verification", "release"; any other kind is update_failed). The raw message of the check is not in it: it is in the log, written
// once by the service of the update, and a message of GitHub or of a download can carry an address.
func UpdateCheck(kind string) *apperr.Error {
	code, ok := updateCodes[kind]
	if !ok {
		code = apperr.CodeUpdateFailed
	}
	return apperr.New(code)
}
