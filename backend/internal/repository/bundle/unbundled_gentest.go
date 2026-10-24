package bundle

// The generation test screen's history (spec/architecture/gentest/er-gentest.md) belongs to no Project: its files are in the
// data directory, outside the assets, and its tables hold plain TEXT ids of Projects. A bundle never carries it. These
// entries are added here, not in the map literal of repository.go, which is at its line limit.
func init() {
	const reason = "the generation test screen's history belongs to no Project and its files are in the data directory, not the assets (spec/architecture/gentest/er-gentest.md)"
	for _, table := range []string{"gentest_batch", "gentest_input", "gentest_result", "gentest_prompt", "gentest_import"} {
		unbundledTables[table] = reason
	}
}
