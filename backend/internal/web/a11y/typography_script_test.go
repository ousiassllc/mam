package a11y_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	// A class a script gives an element it builds: `li.className = '...'`, `` li.className = `...` `` and `li.classList.add('...', '...')`.
	scriptClassName = regexp.MustCompile("(?s)\\b(\\w+)\\.className\\s*=\\s*(?:'([^']*)'|\"([^\"]*)\"|`([^`]*)`)")
	scriptClassAdd  = regexp.MustCompile(`\b(\w+)\.classList\.add\(([^)]*)\)`)
	createdElement  = regexp.MustCompile(`\b(?:const|let|var)\s+(\w+)(?:\s*:\s*[\w<>]+)?\s*=\s*(?:document\.)?createElement\(\s*['"](\w+)['"]`)
)

// A script that builds an element (document.createElement, then className / classList.add) is not a <li class="..."> in a template, so the checks of the tags above
// do not see it: a list item, a list, a field, a button or a warning in text-xs that a script puts on the page is 12px all the same (T3, #1202). The tag of the
// element is the one the variable was created with (or its own name: li, ul, button...).
func TestSources_ScriptBuiltTextToReadIsAtLeast14px(t *testing.T) {
	tagOf := func(created map[string]string, name string) string {
		if tag, ok := created[name]; ok {
			return tag
		}
		return name
	}
	check := func(path, src string, at int, name, classes string, created map[string]string) {
		if !smallText.MatchString(" " + classes + " ") {
			return
		}
		tag := tagOf(created, name)
		line := strings.Count(src[:at], "\n") + 1
		switch {
		case tag == "ul" || tag == "ol" || tag == "li" || tag == "input" || tag == "select" || tag == "textarea" || tag == "button":
			t.Errorf("%s:%d: the script builds a <%s> in text-xs: a list, a field and a button are text-sm or larger (T3)", path, line, tag)
		case (tag == "p" || tag == "pre" || tag == "dd") && alerting.MatchString(" "+classes+" "):
			t.Errorf("%s:%d: the script builds a <%s> that is a warning or an error in text-xs: text-sm or larger (T3)", path, line, tag)
		}
	}
	scanned := 0
	for _, path := range colorSources(t) {
		if !strings.HasSuffix(path, ".ts") {
			continue
		}
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		src := string(raw)
		created := map[string]string{}
		for _, m := range createdElement.FindAllStringSubmatch(src, -1) {
			created[m[1]] = m[2]
		}
		for _, m := range scriptClassName.FindAllStringSubmatchIndex(src, -1) {
			scanned++
			classes := ""
			for g := 4; g < 10; g += 2 {
				if m[g] >= 0 {
					classes = src[m[g]:m[g+1]]
				}
			}
			check(path, src, m[0], src[m[2]:m[3]], classes, created)
		}
		for _, m := range scriptClassAdd.FindAllStringSubmatchIndex(src, -1) {
			scanned++
			check(path, src, m[0], src[m[2]:m[3]], strings.NewReplacer("'", " ", `"`, " ", "`", " ", ",", " ").Replace(src[m[4]:m[5]]), created)
		}
	}
	require.Greater(t, scanned, 5, "the walk found the classes a script gives")
}
