// Package pagination is the page navigation of a list that is read a page at a time (Issue #1079): the links to the first, previous, next and last
// page and to the pages around the current one, with the line "N 件中 a–b 件". The generation test history uses it. It is a package of its own so that
// molecules stays inside its line limit.
package pagination

import (
	"fmt"
	"strconv"
)

// Gap is the entry of Window that stands for the pages left out ("…").
const Gap = 0

// around is how many pages are shown on each side of the current one.
const around = 2

// Props is one navigation.
type Props struct {
	// ID is the prefix of the ids of the links (the id keeps the focus on the link after a swap) and the data-testid of the navigation.
	ID string
	// Target is the DOM id of the element that a page replaces (outerHTML), and the page is pushed into the browser history.
	Target string
	// Current is the page shown (from 1), Last the number of pages, Total the number of items and PerPage the items of a full page.
	Current, Last, Total, PerPage int
	// Href is the address of a page: the link's href (it works without a script) and what htmx reads.
	Href func(page int) string
}

// Window lists the pages to show as links: the first, the last and the ones within 2 of current; a run of two or more pages left out is one Gap (a
// single page that would be left out is shown, so that a "…" never hides just one number).
func Window(current, last int) []int {
	var out []int
	prev := 0
	for p := 1; p <= last; p++ {
		if p != 1 && p != last && (p < current-around || p > current+around) {
			continue
		}
		switch p - prev {
		case 1:
		case 2:
			out = append(out, prev+1)
		default:
			out = append(out, Gap)
		}
		out = append(out, p)
		prev = p
	}
	return out
}

// Summary is "N 件中 a–b 件（M / L ページ）": what is shown of how much there is, and where.
func (p Props) Summary() string {
	from := (p.Current-1)*p.PerPage + 1
	to := min(p.Current*p.PerPage, p.Total)
	return fmt.Sprintf("%d 件中 %d–%d 件（%d / %d ページ）", p.Total, from, to, p.Current, p.Last)
}

func (p Props) id(suffix string) string { return p.ID + "-" + suffix }

func (p Props) pageID(page int) string { return p.id(strconv.Itoa(page)) }

func pageText(page int) string { return strconv.Itoa(page) }

func pageLabel(page int) string { return strconv.Itoa(page) + " ページ目" }

// swap replaces the target and brings its top into view: a page is read from the foot of the list, where the navigation is.
func swap(p Props) string { return "outerHTML show:#" + p.Target + ":top" }
