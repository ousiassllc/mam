// Package licenses is the "第三者のライセンス" category of the global settings screen (spec/components/settings-screen-licenses.md):
// the third-party software of MAM in groups, each entry a <details> that loads its license text the first time it is opened. The
// screen draws whatever groups the data holds (internal/licenses); adding an entry is adding data, not a change here.
package licenses

// Description is what the category says behind its "?" button.
const Description = "MAM が使っているサードパーティのソフトウェアと、そのライセンス文です。項目を開くと全文が読めます。\nMAM 自身のライセンスは AGPL-3.0 です。同じ内容は、インストール先の THIRD_PARTY_NOTICES.md にもあります。\nローカルの音声・口パク（GPT-SoVITS・MuseTalk）は MAM に同梱せず、利用者が取得するものです。その出典とライセンスは「同梱の補足・利用者が取得するもの」にあります。"

// Data is the category: the groups in order.
type Data struct {
	Groups []Group
}

// Group is a heading and its entries.
type Group struct {
	Name  string
	Items []Item
}

// Item is one entry: its title and where its text is read from (paths.LicenseText).
type Item struct {
	ID, Title, TextURL string
}

// Count is the number of entries.
func (d Data) Count() int {
	n := 0
	for _, g := range d.Groups {
		n += len(g.Items)
	}
	return n
}
