package localllm

import "strings"

// StripThinking removes the reasoning that some models write in the answer: "<think>…</think>" anywhere, the text before a closing tag
// whose opening tag the chat template put in the prompt (DeepSeek R1 and its distills), and a block that was never closed because the answer ran
// out of tokens in the middle of it. The rest is returned as it is.
func StripThinking(s string) string {
	const open, closing = "<think>", "</think>"
	for {
		o, c := strings.Index(s, open), strings.Index(s, closing)
		switch {
		case c >= 0 && (o < 0 || c < o):
			s = s[c+len(closing):]
		case o >= 0 && c > o:
			s = s[:o] + s[c+len(closing):]
		case o >= 0:
			return s[:o]
		default:
			return s
		}
	}
}
