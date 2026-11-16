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

// KeyShortage is one missing API key: the provider it belongs to, the models it would let the assignment use (the distinct ones, in the order of the screen), and
// how many places of the assignment (a slot of a route, primary or alternative) name a model of it: the banner lists the biggest shortage first.
type KeyShortage struct {
	Provider string
	Models   []string
	Count    int
}

// KeyBanner is the one banner that says the saved assignment names models whose API key is not registered. It counts rows of the two tables: Rows is how many
// there are (image purposes and Quality Policies together), Blocked how many cannot run at all (neither the primary nor the alternative can be used and an API key
// is missing), Covered how many run on their alternative because the API key of the primary is missing. Shortages say which key lets which models be used (the
// biggest first), and Route is the screen that registers the keys.
type KeyBanner struct {
	Rows, Blocked, Covered int
	Route                  string
	Shortages              []KeyShortage
}
