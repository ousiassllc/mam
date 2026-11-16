package media

// Chip is the small label beside a model that cannot be used now: Label is the short word ("キー未登録"), Title the full reason (the tooltip and the text a
// screen reader gets). Route, when set, is the screen that fixes it and makes the chip a link; it is empty for a missing API key, which the banner (KeyBanner)
// answers once for all of them. The zero Chip is no chip.
type Chip struct{ Label, Title, Route string }

// The kinds of a RowNote.
const (
	// NoteFallback: the primary cannot be used and the alternative can, so the route runs on the alternative.
	NoteFallback = "fallback"
	// NoteSubstitute: no model of the route can be used, and a generation would run on a model that matches the conditions (a default route).
	NoteSubstitute = "substitute"
	// NoteBlocked: no model of the route can be used, and a generation would be refused.
	NoteBlocked = "blocked"
)

// RowNote is what a row says about what a generation would do with it, when its saved models cannot all be used. The zero RowNote is no note.
type RowNote struct{ Kind, Text string }

// KeyShortage is one missing API key: the provider it belongs to, the models it would let the assignment use (the distinct ones, in the order of the screen),
// how many places of the assignment name them (Count: a slot of a route, primary or alternative), and how many of those places are a primary that the route
// does without because its alternative runs (Covered).
type KeyShortage struct {
	Provider string
	Models   []string
	Count    int
	Covered  int
}

// KeyBanner is the one banner that says the saved assignment names models whose API key is not registered: Total is how many places cannot be used, Shortages
// say which key lets how many of them be used (the biggest first), and Route is the screen that registers the keys.
type KeyBanner struct {
	Total     int
	Route     string
	Shortages []KeyShortage
}
