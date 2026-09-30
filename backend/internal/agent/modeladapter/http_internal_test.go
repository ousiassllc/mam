package modeladapter

import "testing"

func TestOwnsURLOnlyAcceptsTheVendorsHosts(t *testing.T) {
	tests := []struct {
		base, target string
		want         bool
	}{
		{"https://api.bfl.ai", "https://api.bfl.ai/v1/get_result?id=1", true},
		{"https://api.bfl.ai", "https://api.eu.bfl.ai/v1/get_result?id=1", true},
		{"https://api.bfl.ai", "https://API.US.BFL.AI/x", true},
		{"https://api.bfl.ai", "https://evil.example/v1/get_result", false},
		{"https://api.bfl.ai", "https://bfl.ai.evil.example/", false},
		{"https://api.bfl.ai", "https://evilbfl.ai/", false},
		{"https://api.bfl.ai", "http://api.bfl.ai/x", false},
		{"https://api.bfl.ai", "https://api.bfl.ai:8443/x", false},
		{"https://api.bfl.ai", "https://user@api.bfl.ai/x", false},
		{"https://api.bfl.ai", "https://127.0.0.1/x", false},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080/poll", true},
		{"http://127.0.0.1:8080", "http://127.0.0.2:8080/poll", false},
		{"http://127.0.0.1:8080", "http://127.0.0.1:9090/poll", false},
		{"https://bfl.ai", "https://bfl.ai/x", true},
		{"https://bfl.ai", "https://eu.bfl.ai/x", true},
		{"", "https://api.bfl.ai/x", false},
	}
	for _, tt := range tests {
		if got := (api{baseURL: tt.base}).ownsURL(tt.target); got != tt.want {
			t.Errorf("ownsURL(base %q, %q) = %v, want %v", tt.base, tt.target, got, tt.want)
		}
	}
}
