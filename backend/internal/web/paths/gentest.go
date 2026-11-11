package paths

// GenerationTest is the generation test screen, "/generation-test" (spec/architecture/gentest/gentest-screen.md): images and videos
// generated outside any Project, models and prompts compared side by side. GET answers the page; "?model=<id>" opens it with
// that model chosen (the model manager links to it that way).
func GenerationTest() string { return "/generation-test" }

// GenerationTestPart is "/generation-test/<rest...>": a section or an action of the screen. Every segment is escaped.
func GenerationTestPart(rest ...string) string { return join(GenerationTest(), rest...) }

// GenerationTestChat is the chat of the single generation, "/generation-test/chat" (#1138): talk to the LLM about what to make, see the models
// compared and the estimate, confirm on a card and read the results in the thread. "/messages" (POST) sends a message, "/proposals/<id>/confirm" and
// "/proposals/<id>/dismiss" (POST) answer a confirmation card and "/batches/<id>" (GET) is the results block that asks for itself while the batch runs.
func GenerationTestChat(rest ...string) string {
	return GenerationTestPart(append([]string{"chat"}, rest...)...)
}

// GenerationTestHistory is the history of the generation test, "/generation-test/history".
func GenerationTestHistory() string { return GenerationTestPart("history") }

// GenerationTestPrompts is the prompt history of the generation test, "/generation-test/prompts".
func GenerationTestPrompts() string { return GenerationTestPart("prompts") }

// GenerationTestMatch is the prompt match of the generation test, "/generation-test/match" (#916): one reference image, a prompt
// written for it and fixed until the generated image looks like it. "?ref=<source>:<project>:<id>" opens it with that image.
func GenerationTestMatch() string { return GenerationTestPart("match") }

// GenerationTestInpaint is the partial redraw screen, "/generation-test/inpaint" (#974).
func GenerationTestInpaint() string { return GenerationTestPart("inpaint") }

// GenerationTestExport is the ZIP export of results of the history, "/generation-test/export" (#932). GET answers the ZIP:
// "?id=<result id>" (repeated) names results, "?latest=<n>" the newest n, with "&model=&media=&favorite=1&commercial=&batch=".
func GenerationTestExport() string { return GenerationTestPart("export") }

// GenerationTestPoses is the pose picker of the generation test, "/generation-test/poses" (#986): GET answers the body of its dialog (the
// gallery, or the offer to install the pose library); "/results" the poses that match the filters, "/install" (POST) installs the library and
// "/image/<picture id>" is one picture of it.
func GenerationTestPoses(rest ...string) string {
	return GenerationTestPart(append([]string{"poses"}, rest...)...)
}

// ModelPicker is the model picker, "/model-picker": the list of models a field chooses one from, with the filter of the models (#1011). ModelPickerSource is
// "/model-picker/<source>": the picker of one kind of field (the match form's model, a Shot's video model, the LLM of an Agent...). GET answers the dialog's body;
// with "part=results" it answers the cards alone, for a changed filter.
func ModelPicker() string { return "/model-picker" }

// ModelPickerSource is "/model-picker/<source>".
func ModelPickerSource(source string) string { return join(ModelPicker(), source) }
