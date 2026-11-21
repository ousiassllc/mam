package a11y_test

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// muteNode is an element (tag != "") or a text (tag == "") of a screen, from its source (a templ file, a Lit template) or from the HTML a screen rendered.
// The colour of a text is decided by the nearest element of its own or of its ancestors that says a colour, so the tree keeps the parents.
type muteNode struct {
	tag     string
	classes map[string]bool
	text    string
	line    int
	parent  *muteNode
	kids    []*muteNode
}

// textColors are the classes that say a colour of the text: an element that has one of them is not --mute whatever its ancestors say.
var textColors = map[string]bool{
	"text-ink": true, "text-bone": true, "text-accent": true, "text-danger": true, "text-warning": true, "text-success": true, "text-info": true,
}

// muted says whether the text of n is --mute: the nearest element that names a text colour (itself first) is text-mute.
func (n *muteNode) muted() bool {
	for e := n; e != nil; e = e.parent {
		if e.classes["text-mute"] {
			return true
		}
		for c := range e.classes {
			if textColors[c] {
				return false
			}
		}
	}
	return false
}

// inlineTags are the elements that run on in the text of their parent: a sentence in a <span> inside a <p> is a sentence of the <p>. A <span class="block">
// is a block of its own.
var inlineTags = map[string]bool{
	"a": true, "span": true, "code": true, "strong": true, "em": true, "b": true, "i": true, "output": true, "small": true, "abbr": true, "kbd": true, "time": true, "mark": true, "label": true,
}

// ownText is what n says as a block: its texts, and those of its inline children that do not say a colour of their own (a child that does is another text).
func (n *muteNode) ownText() string {
	var sb strings.Builder
	for _, k := range n.kids {
		switch {
		case k.tag == "":
			sb.WriteString(k.text)
		case inlineTags[k.tag] && !k.classes["block"] && !k.sayColor():
			sb.WriteString(k.ownText())
		}
	}
	return sb.String()
}

func (n *muteNode) sayColor() bool {
	for c := range n.classes {
		if textColors[c] || c == "text-mute" {
			return true
		}
	}
	return false
}

var (
	parentheses = regexp.MustCompile(`（[^）]*）|\([^)]*\)`)
	sentenceEnd = "。"
)

// sentences counts the sentences of a text: the parts between full stops, outside parentheses, that say something ("A。B" is two sentences, as is "A。B。"; "A。" is
// one). A run of phrases in parentheses ("（0.05〜1。空欄は未設定）") is a label's hint, not a sentence.
func sentences(text string) int {
	n := 0
	for _, part := range strings.Split(parentheses.ReplaceAllString(text, ""), sentenceEnd) {
		part = strings.TrimSpace(part)
		if strings.IndexFunc(part, unicode.IsLetter) >= 0 && !strings.HasSuffix(part, ":") && !strings.HasSuffix(part, "：") { // "保存先:" is the label of a value, not a sentence
			n++
		}
	}
	return n
}

// mutedSentences walks the tree and lists the elements whose own text is --mute (by their own class or an ancestor's) and says two sentences or more.
func mutedSentences(root *muteNode) []string {
	var found []string
	var walk func(n *muteNode)
	walk = func(n *muteNode) {
		if n.tag != "" && n.muted() {
			if count := sentences(n.ownText()); count >= 2 {
				found = append(found, fmt.Sprintf("line %d <%s>: %d sentences: %q", n.line, n.tag, count, short(n.ownText())))
			}
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(root)
	return found
}

func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

// ---- the tree of a rendered HTML ----

// parseRendered is the tree of the HTML a screen rendered (what the browser shows: the text of every element, a dialog that is shut included).
func parseRendered(src string) (*muteNode, error) {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return nil, err
	}
	var build func(n *html.Node, parent *muteNode)
	build = func(n *html.Node, parent *muteNode) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case html.TextNode:
				parent.kids = append(parent.kids, &muteNode{text: c.Data, parent: parent})
			case html.ElementNode:
				if c.Data == "script" || c.Data == "style" || c.Data == "template" || c.Data == "option" || c.Data == "svg" {
					continue
				}
				e := &muteNode{tag: c.Data, classes: map[string]bool{}, parent: parent}
				for _, a := range c.Attr {
					if a.Key == "class" {
						for _, w := range strings.Fields(a.Val) {
							e.classes[w] = true
						}
					}
				}
				parent.kids = append(parent.kids, e)
				build(c, e)
			}
		}
	}
	root := &muteNode{classes: map[string]bool{}}
	build(doc, root)
	return root, nil
}

// ---- the tree of a source ----

var (
	sourceTag    = regexp.MustCompile(`(?s)<(/?)([a-zA-Z][a-zA-Z0-9-]*)((?:[^<>"'{$]|"[^"]*"|'[^']*'|\{[^}]*\}|\$\{[^}]*\}|\$)*?)(/?)>`)
	sourceClass  = regexp.MustCompile(`(?s)\bclass=(?:"([^"]*)"|'([^']*)'|\{([^}]*)\})`)
	quotedString = regexp.MustCompile(`"([^"]*)"|'([^']*)'|` + "`([^`]*)`")
	placeholder  = regexp.MustCompile(`\$\{[^}<]*\}|\{[^{}]*\}`)
	templControl = regexp.MustCompile(`\}?\s*\b(?:else if|if|for|switch|else)\b[^\n{]*\{|[{}]`)
	componentUse = regexp.MustCompile(`(?m)^[ \t]*@[\w.]+\(`)
	voidTags     = map[string]bool{"input": true, "br": true, "img": true, "hr": true, "meta": true, "link": true, "source": true, "wbr": true}
)

var htmlTags = map[string]bool{}

func init() {
	for _, t := range strings.Fields(`a abbr article aside audio b blockquote br button canvas caption code dd details dialog div dl dt em fieldset figcaption figure footer form
		h1 h2 h3 h4 h5 h6 header hr i iframe img input kbd label legend li main mark nav ol optgroup option output p path pre progress section select small span strong
		sub summary sup svg table tbody td textarea tfoot th thead time tr ul video`) {
		htmlTags[t] = true
	}
}

// markupTag says whether a tag of a source is an HTML element or a custom element (mam-...).
func markupTag(tag string) bool { return htmlTags[tag] || strings.Contains(tag, "-") }

// classesOf reads the class words of a tag's attributes: the words of class="...", and the strings in class={ ... } / class=${ ... }.
func classesOf(attrs string) map[string]bool {
	set := map[string]bool{}
	for _, m := range sourceClass.FindAllStringSubmatch(attrs, -1) {
		raw := m[1] + m[2]
		if m[3] != "" {
			for _, q := range quotedString.FindAllStringSubmatch(m[3], -1) {
				raw += " " + q[1] + q[2] + q[3]
			}
		}
		for _, w := range strings.Fields(raw) {
			set[strings.Trim(w, `'"${}?:,`)] = true
		}
	}
	return set
}

// withoutComponentCalls drops the `@pkg.Component(...)` calls of a templ file (their arguments are Go, not text) but not the `{ ... }` block that may follow them.
func withoutComponentCalls(src string) string {
	var sb strings.Builder
	for {
		loc := componentUse.FindStringIndex(src)
		if loc == nil {
			sb.WriteString(src)
			return sb.String()
		}
		sb.WriteString(src[:loc[0]])
		depth, i := 1, loc[1]
		for ; i < len(src) && depth > 0; i++ {
			switch src[i] {
			case '(':
				depth++
			case ')':
				depth--
			case '"':
				for i++; i < len(src) && src[i] != '"'; i++ {
					if src[i] == '\\' {
						i++
					}
				}
			}
		}
		src = src[i:]
	}
}

// parseSource is the tree of the markup in a templ file or in the html`...` of a Lit file: the interpolated values are not text (a constant or a
// catalog entry is checked on the screen that renders it), only the words written in the source are.
func parseSource(src string) *muteNode {
	src = withoutComponentCalls(src)
	root := &muteNode{classes: map[string]bool{}}
	cur := root
	pos := 0
	addText := func(end int) {
		text := templControl.ReplaceAllString(placeholder.ReplaceAllString(src[pos:end], ""), "")
		if strings.TrimSpace(text) != "" {
			cur.kids = append(cur.kids, &muteNode{text: text, parent: cur})
		}
	}
	for _, m := range sourceTag.FindAllStringSubmatchIndex(src, -1) {
		addText(m[0])
		pos = m[1]
		closing, tag, attrs, selfClosing := m[3] > m[2], src[m[4]:m[5]], src[m[6]:m[7]], m[9] > m[8]
		if !markupTag(tag) {
			continue // a type argument of TypeScript (`Promise<void>`), not an element
		}
		switch {
		case closing:
			for e := cur; e != nil && e != root; e = e.parent {
				if e.tag == tag {
					cur = e.parent
					break
				}
			}
		default:
			e := &muteNode{tag: tag, classes: classesOf(attrs), parent: cur, line: strings.Count(src[:m[0]], "\n") + 1}
			cur.kids = append(cur.kids, e)
			if !selfClosing && !voidTags[tag] {
				cur = e
			}
		}
	}
	addText(len(src))
	return root
}
