// Package licenses is what the settings screen "第三者のライセンス" lists: the licenses of the third-party software of MAM, as
// groups of entries (spec/components/settings-screen-licenses.md). Where the entries come from is a Source; the screen only
// reads the Catalog, so an entry that is added later (the weights of a local runtime, ...) is data in a Source and no change of
// the screen. The one Source today is THIRD_PARTY_NOTICES.md (ThirdPartyNotices).
package licenses

import (
	"strconv"
	"sync"
)

// Entry is one thing whose license is shown: the heading under which it is listed, the group it is listed in and its text. The
// text is plain text (the screen escapes it and never reads it as markup).
type Entry struct {
	Group string
	Title string
	Body  string
}

// Source is where entries come from. Entries are listed in the order of the sources, and in a source in its own order.
type Source func() []Entry

// Section is an Entry in a Catalog: ID is what the screen asks for its text by.
type Section struct {
	ID string
	Entry
}

// Group is the entries of one group, in order.
type Group struct {
	Name     string
	Sections []Section
}

// sources are the sources of Default. To show more, add a Source here (or an entry to the data of one): the screen is not changed.
var sources = []Source{ThirdPartyNotices}

// Catalog is the entries of some sources, grouped.
type Catalog struct {
	groups []Group
	byID   map[string]Section
}

// New lists the entries of the sources. A group is where its first entry is; an entry without a group is listed in DefaultGroup.
func New(sources ...Source) *Catalog {
	c := &Catalog{byID: map[string]Section{}}
	index := map[string]int{}
	for _, src := range sources {
		for _, e := range src() {
			if e.Group == "" {
				e.Group = DefaultGroup
			}
			gi, ok := index[e.Group]
			if !ok {
				gi = len(c.groups)
				index[e.Group] = gi
				c.groups = append(c.groups, Group{Name: e.Group})
			}
			s := Section{ID: strconv.Itoa(len(c.byID) + 1), Entry: e}
			c.groups[gi].Sections = append(c.groups[gi].Sections, s)
			c.byID[s.ID] = s
		}
	}
	return c
}

// DefaultGroup is the group of an entry that names none.
const DefaultGroup = "その他"

// Groups are the groups in order. The slice is the catalog's own: do not change it.
func (c *Catalog) Groups() []Group { return c.groups }

// Find is the entry with the ID.
func (c *Catalog) Find(id string) (Section, bool) {
	s, ok := c.byID[id]
	return s, ok
}

// Len is the number of entries.
func (c *Catalog) Len() int { return len(c.byID) }

var (
	defaultOnce    sync.Once
	defaultCatalog *Catalog
)

// Default is the catalog of sources, built on first use (parsing the notices takes a few milliseconds).
func Default() *Catalog {
	defaultOnce.Do(func() { defaultCatalog = New(sources...) })
	return defaultCatalog
}
