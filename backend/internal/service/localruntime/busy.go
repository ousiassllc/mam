package localruntime

import "fmt"

// BusyError is ErrBusy with the reason in words: the work that holds the runtime (a model being fetched into it). errors.Is reaches ErrBusy, so every
// caller that maps ErrBusy keeps working; the screens and the MCP tools show Reason (mmview.RefuseRuntime).
type BusyError struct {
	Runtime string
	Reason  string
}

func (e *BusyError) Error() string { return e.Reason }

// Is makes errors.Is(err, ErrBusy) true.
func (e *BusyError) Is(target error) bool { return target == ErrBusy }

// heldBy is the *BusyError that refuses to remove, replace or stop the server of runtime id while Deps.HeldBy names something that uses it (nil: nothing does).
// The server is not stopped and no file is touched: the work that uses it would fail half way, and its files are the ones being written.
func (s *Service) heldBy(id string) error {
	if s.HeldBy == nil {
		return nil
	}
	what := s.HeldBy(id)
	if what == "" {
		return nil
	}
	return &BusyError{Runtime: id, Reason: fmt.Sprintf("%s を取得している最中です。取得が終わってから、または取得を中断してからやり直してください", what)}
}
