package llm

import "strconv"

// bannerTitle says how many of the Agents that run cannot run for want of an API key and how many run on their alternative. With none that cannot run it only says that some
// run on their alternative.
func bannerTitle(b KeyBanner) string {
	if b.Blocked == 0 {
		return "一部は代替モデルで動いています"
	}
	title := "API キーが足りないため、Agent " + strconv.Itoa(b.Rows) + "件のうち " + strconv.Itoa(b.Blocked) + " 件で使えません"
	if b.Covered > 0 {
		title += "。" + strconv.Itoa(b.Covered) + " 件は代替モデルで動いています"
	}
	return title
}

// maxNamed is how many Agent names a line of the banner names; the rest are a count.
const maxNamed = 2

// shortageText says which Agents the key lets use their models: the names of the first two and, when there are more, how many in all.
func shortageText(s KeyShortage) string {
	names := s.Agents
	if len(names) > maxNamed {
		names = names[:maxNamed]
	}
	who := ""
	for i, n := range names {
		if i > 0 {
			who += "・"
		}
		who += n
	}
	if len(s.Agents) > maxNamed {
		who += " など " + strconv.Itoa(len(s.Agents)) + " 件"
	}
	return "のキーを入れると、" + who + "が使えるようになります"
}
