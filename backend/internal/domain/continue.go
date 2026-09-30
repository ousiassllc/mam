package domain

// StateAnalysis is what Continue reads from a completed Shot before it proposes what happens next
// (spec/spec.md §15): the final frame and the Story, Character, Position, Emotion, Object,
// Camera, Lighting, Motion, Dialogue and Music State at the end of the Shot. Every value is one
// short Japanese text; a State the Shot does not show is "".
type StateAnalysis struct {
	FinalFrame string `json:"final_frame"`
	Story      string `json:"story"`
	Character  string `json:"character"`
	Position   string `json:"position"`
	Emotion    string `json:"emotion"`
	Object     string `json:"object"`
	Camera     string `json:"camera"`
	Lighting   string `json:"lighting"`
	Motion     string `json:"motion"`
	Dialogue   string `json:"dialogue"`
	Music      string `json:"music"`
}

// AnalysisRow is one labelled line of a StateAnalysis.
type AnalysisRow struct{ Label, Value string }

// Rows lists the analysed States in display order, skipping the ones that are empty.
func (a StateAnalysis) Rows() []AnalysisRow {
	var rows []AnalysisRow
	for _, r := range []AnalysisRow{
		{"最終フレーム", a.FinalFrame}, {"ストーリー", a.Story}, {"キャラクター", a.Character},
		{"位置", a.Position}, {"感情", a.Emotion}, {"オブジェクト", a.Object}, {"カメラ", a.Camera},
		{"ライティング", a.Lighting}, {"動き", a.Motion}, {"セリフ", a.Dialogue}, {"音楽", a.Music},
	} {
		if r.Value != "" {
			rows = append(rows, r)
		}
	}
	return rows
}

// ContinuePlan is the next Shot, or the next Scene with its first Shot, that Continue adds after
// a completed Shot.
type ContinuePlan struct {
	// NewScene is true when the story moves to a new Scene; Scene then describes it.
	NewScene bool
	Scene    PlannedScene
	Shot     PlannedShot
}
