// Package exportbar is the bar of the history of the generation test that saves results as a ZIP (#932). It is a package of its own for
// the line limit of organisms/gentest; the tap-to-choose behaviour of the cards it belongs to is static/src/components/gentest/export-pick.ts.
package exportbar

// Data is what the bar needs: where the ZIP is asked for and the filter of the history that the "newest N" form carries along.
type Data struct {
	// URL is paths.GenerationTestExport.
	URL string
	// Model, Media and Favorite are the filter of the list ("" and false: not filtered); Commercial is its commercial-use filter
	// ("all", "ok" or "ok_or_conditional"), which the page can change without asking the server (the hidden field follows the radio).
	Model, Media, Commercial string
	Favorite                 bool
}
