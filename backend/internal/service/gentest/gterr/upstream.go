package gterr

// UpstreamError is a refusal because the Reasoning LLM could not do what the request needs (the translation of the prompts into
// English): nothing was generated or billed. Message is shown to the user; Err is the cause, for the log (it is not shown).
type UpstreamError struct {
	Message string
	Err     error
}

func (e *UpstreamError) Error() string { return "gentest: upstream: " + e.Message }
func (e *UpstreamError) Unwrap() error { return e.Err }
