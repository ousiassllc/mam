package exeinstall

import "time"

// NextError is nextError, for the tests of its classification.
var NextError = nextError

// SwapAttempts is how many times Swap tries one rename.
const SwapAttempts = swapAttempts

// SwapWith is Swap with the rename and the wait between attempts given by the test.
func SwapWith(rename func(from, to string) error, sleep func(time.Duration), newDir, dir, oldDir string) error {
	return renamer{rename: rename, sleep: sleep}.swap(newDir, dir, oldDir)
}
