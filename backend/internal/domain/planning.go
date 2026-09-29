package domain

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Beat is one part of the structure the Producer Agent lays out.
type Beat struct {
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

// Concept is the output of the Producer Agent: the concept and structure developed
// from the Creative Brief (spec/spec.md §4.3).
type Concept struct {
	Title     string `json:"title"`
	Logline   string `json:"logline"`
	Concept   string `json:"concept"`
	Tone      string `json:"tone,omitempty"`
	Structure []Beat `json:"structure"`
}

// ScriptScene is one scene of the script written by the Writer Agent.
type ScriptScene struct {
	Title       string   `json:"title"`
	Location    string   `json:"location,omitempty"`
	Time        string   `json:"time,omitempty"`
	Objective   string   `json:"objective,omitempty"`
	Description string   `json:"description"`
	Dialogue    []string `json:"dialogue,omitempty"` // "speaker: line" or narration
}

// Script is the output of the Writer Agent.
type Script struct {
	Title  string        `json:"title"`
	Scenes []ScriptScene `json:"scenes"`
}

// ScriptDoc is what script_version.content stores: the script together with the
// concept it was written from, so a version can be read on its own.
type ScriptDoc struct {
	Concept Concept `json:"concept"`
	Script  Script  `json:"script"`
}

// ScriptVersion is one row of script_version. A change request creates a new version.
type ScriptVersion struct {
	ID            string
	ProjectID     string
	VersionNumber int
	Doc           ScriptDoc
	CreatedAt     time.Time
}

// PlannedShot is one Shot proposed by the Director Agent.
type PlannedShot struct {
	Objective       string  `json:"objective"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// PlannedScene is one Scene proposed by the Director Agent with its Shots.
type PlannedScene struct {
	Title     string        `json:"title"`
	Location  string        `json:"location,omitempty"`
	Time      string        `json:"time,omitempty"`
	Objective string        `json:"objective,omitempty"`
	Shots     []PlannedShot `json:"shots"`
}

// ScenePlan is the output of the Director Agent (Scene / Shot Planning).
type ScenePlan struct {
	Scenes []PlannedScene `json:"scenes"`
}

// ShotCount is the number of Shots over all Scenes.
func (p ScenePlan) ShotCount() int {
	n := 0
	for _, s := range p.Scenes {
		n += len(s.Shots)
	}
	return n
}

// ShotStoryboard is the storyboard frame the Storyboard Agent draws for one Shot. It
// is stored under the "storyboard" key of shot.shot_spec; the Canonical Shot Spec
// fields are added next to it by the Prompt Planner.
type ShotStoryboard struct {
	Description string `json:"description"` // what the frame shows
	Framing     string `json:"framing,omitempty"`
	Camera      string `json:"camera,omitempty"`
	Composition string `json:"composition,omitempty"`
	Action      string `json:"action,omitempty"`
	Dialogue    string `json:"dialogue,omitempty"`
}

// Storyboard holds the frames of a ScenePlan: Shots[i][j] belongs to Scenes[i].Shots[j].
type Storyboard struct {
	Shots [][]ShotStoryboard
}

// Scene is a row of scene: one scene of the video. State is the sub-state within the
// Core State Machine.
type Scene struct {
	ID         string
	ProjectID  string
	OrderIndex int
	Title      string
	Location   string // name of the Location, "" when unknown
	LocationID string
	Time       string
	Objective  string
	State      ShotState
}

// Shot is a row of shot. ShotSpec is the JSON of shot.shot_spec; unknown keys are
// kept as they are so writers of other fields never lose each other's data.
type Shot struct {
	ID             string
	SceneID        string
	OrderIndex     int
	Objective      string
	Duration       float64 // seconds
	ShotSpec       json.RawMessage
	State          ShotState
	SelectedTakeID string
}

// Storyboard returns the storyboard frame of the Shot and whether it has one.
func (s Shot) Storyboard() (ShotStoryboard, bool) {
	var spec struct {
		Storyboard *ShotStoryboard `json:"storyboard"`
	}
	if err := json.Unmarshal(s.ShotSpec, &spec); err != nil || spec.Storyboard == nil {
		return ShotStoryboard{}, false
	}
	return *spec.Storyboard, true
}

// EncodeShotSpec builds the shot_spec JSON holding the storyboard frame. A zero
// frame gives the empty spec "{}".
func EncodeShotSpec(frame *ShotStoryboard) json.RawMessage {
	if frame == nil {
		return json.RawMessage("{}")
	}
	raw, err := json.Marshal(map[string]any{"storyboard": frame})
	if err != nil { // only strings: Marshal cannot fail
		panic(err)
	}
	return raw
}

// TakeStatus is take.status.
type TakeStatus string

// Take statuses.
const (
	TakeCandidate TakeStatus = "candidate"
	TakeAIPick    TakeStatus = "ai_pick"
	TakeSelected  TakeStatus = "selected"
	TakeRejected  TakeStatus = "rejected"
)

// Take is a row of take, seen from its Shot.
type Take struct {
	ID              string
	ShotID          string
	PromptVersionID string
	ModelName       string
	FilePath        string // relative to the assets directory; "" until the file exists
	Status          TakeStatus
	CreatedAt       time.Time
}

// Summary is the text of the script's Approval card: a headline such as
// "脚本 v1「タイトル」（3 Scene / 15秒）", the concept and the scenes in order.
// duration is the Brief Duration ("" when unknown).
func (v ScriptVersion) Summary(duration string) string {
	var b strings.Builder
	b.WriteString("脚本 v")
	b.WriteString(strconv.Itoa(v.VersionNumber))
	if t := strings.TrimSpace(v.Doc.Script.Title); t != "" {
		b.WriteString("「" + t + "」")
	}
	b.WriteString("（" + strconv.Itoa(len(v.Doc.Script.Scenes)) + " Scene")
	if d := strings.TrimSpace(duration); d != "" {
		b.WriteString(" / " + d)
	}
	b.WriteString("）")
	if l := strings.TrimSpace(v.Doc.Concept.Logline); l != "" {
		b.WriteString("\nコンセプト: " + l)
	}
	for i, sc := range v.Doc.Script.Scenes {
		b.WriteString("\n" + strconv.Itoa(i+1) + ". " + sc.Title + " — " + sc.Description)
	}
	return b.String()
}
