package chatops

import "context"

type shotKey struct{}

// WithShot puts the Shot the direction of this turn is about into ctx, for the operations that act on a Shot (commandinterpreter.Spec.Shot: the audio of a Shot).
func WithShot(ctx context.Context, shotID string) context.Context {
	return context.WithValue(ctx, shotKey{}, shotID)
}

// ShotID is the Shot the direction is about (WithShot); "" when none is known.
func ShotID(ctx context.Context) string {
	id, _ := ctx.Value(shotKey{}).(string)
	return id
}
