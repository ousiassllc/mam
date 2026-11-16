// Package inputcard is an input of a batch as the page of a result lists it: a picture for an image, a player for the audio of a speech-to-video model
// (Issue #1153). It is a package of its own so that organisms/gentest stays inside its line limit.
package inputcard

// Card is an input of a batch: its role in words, where it came from, the address of its file, and whether it is audio (no picture then).
type Card struct {
	Role   string
	Origin string
	URL    string
	Audio  bool
}
