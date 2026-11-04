package facesim

// Availability is implemented by an Engine that can be off now and on later (holder.Holder: the models are installed while the app runs, #1026).
type Availability interface {
	// Available reports whether Faces would work now.
	Available() bool
}

// Usable reports whether the engine can find faces now: false for nil and for an Engine that says it is off. Callers ask it before they
// spend work (sampling the frames of a video, decoding the pictures) that Faces would only throw away.
func Usable(e Engine) bool {
	if e == nil {
		return false
	}
	if a, ok := e.(Availability); ok {
		return a.Available()
	}
	return true
}
