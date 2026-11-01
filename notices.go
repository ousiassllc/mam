// Package mam is the root of the module. It holds the files of the repository root that the binary embeds, because go:embed
// cannot reach a parent directory: THIRD_PARTY_NOTICES.md stays where scripts/third-party-notices.sh writes it and where the
// installer takes it from, and the in-app screen "第三者のライセンス" (spec/components/settings-screen-licenses.md) reads the very
// same bytes, so the two cannot drift.
package mam

import _ "embed"

// ThirdPartyNotices is THIRD_PARTY_NOTICES.md of the repository root, as it was when the binary was built.
//
//go:embed THIRD_PARTY_NOTICES.md
var ThirdPartyNotices string
