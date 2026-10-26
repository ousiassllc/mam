package appserver

import (
	"strings"
	"testing"
)

func TestTheEndOfStandardErrorIsRedactedBeforeItIsShown(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdefghijk"
	got := exitText(1, "level=debug authorization: Bearer secret-value-123\nid_token="+jwt+"\nfatal: no such file\n")
	if strings.Contains(got, jwt) || strings.Contains(got, "secret-value-123") {
		t.Fatalf("a credential reached the text: %q", got)
	}
	if !strings.Contains(got, "fatal: no such file") || !strings.Contains(got, "終了コード 1") {
		t.Errorf("the ordinary text must stay: %q", got)
	}
}
