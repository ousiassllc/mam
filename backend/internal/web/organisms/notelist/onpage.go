// Package notelist is the list of the notes of a Project or a Series (the notes screen).
package notelist

import "net/http"

// OnPage is the ids of the notes the page that sent r shows (OnPageField): in the query of a DELETE, in
// the form of the other methods.
func OnPage(r *http.Request) []string {
	_ = r.ParseForm() // a body that does not parse leaves no ids: every note is then drawn in full
	return r.Form[OnPageField]
}
