package version

import "testing"

// The app checks this repository's own Releases (Issue #827): a separate release-only repository
// would need its own token and would leave the installed apps pointing at a repository that has
// no releases.
func TestUpdateSourceIsThisRepository(t *testing.T) {
	if GitHubOwner != "ousiassllc" || GitHubRepo != "mam" {
		t.Fatalf("update source = %s/%s, want ousiassllc/mam", GitHubOwner, GitHubRepo)
	}
}
