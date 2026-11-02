package paths

// GenerationTest is the generation test screen, "/generation-test" (spec/architecture/gentest/gentest-screen.md): images and videos
// generated outside any Project, models and prompts compared side by side. GET answers the page; "?model=<id>" opens it with
// that model chosen (the model manager links to it that way).
func GenerationTest() string { return "/generation-test" }

// GenerationTestPart is "/generation-test/<rest...>": a section or an action of the screen. Every segment is escaped.
func GenerationTestPart(rest ...string) string { return join(GenerationTest(), rest...) }

// GenerationTestHistory is the history of the generation test, "/generation-test/history".
func GenerationTestHistory() string { return GenerationTestPart("history") }

// GenerationTestPrompts is the prompt history of the generation test, "/generation-test/prompts".
func GenerationTestPrompts() string { return GenerationTestPart("prompts") }

// GenerationTestMatch is the prompt match of the generation test, "/generation-test/match" (#916): one reference image, a prompt
// written for it and fixed until the generated image looks like it. "?ref=<source>:<project>:<id>" opens it with that image.
func GenerationTestMatch() string { return GenerationTestPart("match") }
