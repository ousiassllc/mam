package domain

import (
	"encoding/json"
	"time"
)

// MessageRole is chat_message.role.
type MessageRole string

// Message roles. Users only write plain text; cards come from the assistant.
const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
)

// MessageKind is chat_message.kind: a plain message or one of the six cards (spec/spec.md §23.2).
type MessageKind string

// Message kinds.
const (
	KindText       MessageKind = "text"
	KindQuestion   MessageKind = "question"
	KindApproval   MessageKind = "approval"
	KindKeyframe   MessageKind = "keyframe"
	KindTake       MessageKind = "take"
	KindProgress   MessageKind = "progress"
	KindEscalation MessageKind = "escalation"
)

// ApprovalStatus is chat_message.approval_status (only set on approval cards).
type ApprovalStatus string

// Approval statuses.
const (
	ApprovalPending          ApprovalStatus = "pending"
	ApprovalApproved         ApprovalStatus = "approved"
	ApprovalChangesRequested ApprovalStatus = "changes_requested"
)

// chat_message.ref_type values understood by the chat. Cards refer to an entity
// (er.md §4); Approval cards use the artifact types of CheckpointRefType.
const (
	RefBriefField    = "brief_field" // Question card: ref_id is the BriefField being asked
	RefAsset         = "asset"       // Keyframe card: ref_id is the asset ID
	RefTake          = "take"        // Take card: ref_id is the take ID
	RefJob           = "job"         // Progress card: ref_id is the job ID (status is read live)
	RefShot          = "shot"        // Escalation card: ref_id is the shot ID (optional)
	RefNotePromotion = "note_promotion"
	RefContinue      = "continue" // Question card of Continue: ref_id is the completed shot ID
)

// ChatMessage is one entry of a Project's chat thread. The thread is append-only;
// only ApprovalStatus changes, when the user responds to an approval card.
type ChatMessage struct {
	ID                 string
	ProjectID          string
	Role               MessageRole
	Kind               MessageKind
	Content            string
	ReplyToMessageID   string // "" when not a reply
	RefType            string
	RefID              string
	ApprovalStatus     ApprovalStatus // "" unless Kind == KindApproval
	AttachmentAssetIDs []string
	CreatedAt          time.Time
}

// IsBlockingApproval reports whether the message is an approval card that holds the
// Project in AWAITING_APPROVAL until answered (Notes promotions never block).
func (m ChatMessage) IsBlockingApproval() bool {
	return m.Kind == KindApproval && m.RefType != RefNotePromotion
}

// CardBody is the JSON stored in content of question and escalation cards.
// The "AIに任せる" button is always added by the renderer and is not stored.
type CardBody struct {
	Text    string   `json:"text"`
	Options []string `json:"options,omitempty"`
}

// EncodeCardBody serialises a question / escalation card body.
func EncodeCardBody(b CardBody) string {
	raw, err := json.Marshal(b)
	if err != nil { // CardBody only holds strings; Marshal cannot fail
		panic(err)
	}
	return string(raw)
}

// DecodeCardBody parses the content of a question / escalation card. Content that
// is not a card body is treated as plain text without options.
func DecodeCardBody(content string) CardBody {
	var b CardBody
	if err := json.Unmarshal([]byte(content), &b); err != nil || b.Text == "" {
		return CardBody{Text: content}
	}
	return b
}

// TakeCardBody is the JSON stored in content of take cards. VideoURL is resolved
// by the producer of the card (a same-origin path); the Take itself is ref_id.
type TakeCardBody struct {
	Title    string   `json:"title"`
	Badge    string   `json:"badge,omitempty"` // e.g. "AI Pick"
	Score    *float64 `json:"score,omitempty"` // overall judge score, 0..1
	Summary  string   `json:"summary,omitempty"`
	VideoURL string   `json:"video_url,omitempty"`
	// PosterURL is the still shown before the video plays; ShotID is the Shot the Take belongs
	// to (the card links to its Take comparison).
	PosterURL string `json:"poster_url,omitempty"`
	ShotID    string `json:"shot_id,omitempty"`
}

// EncodeTakeCardBody serialises a take card body.
func EncodeTakeCardBody(b TakeCardBody) string {
	raw, err := json.Marshal(b)
	if err != nil { // only strings and a float64: Marshal cannot fail for finite scores
		panic(err)
	}
	return string(raw)
}

// DecodeTakeCardBody parses the content of a take card; anything else becomes the title.
func DecodeTakeCardBody(content string) TakeCardBody {
	var b TakeCardBody
	if err := json.Unmarshal([]byte(content), &b); err != nil || b.Title == "" {
		return TakeCardBody{Title: content}
	}
	return b
}

// ThreadMessage is a chat message prepared for rendering: the message plus what the
// card needs that is not stored on the row.
type ThreadMessage struct {
	ChatMessage
	Attachments []Asset
	ReplyTo     *ChatMessage // the message this one answers, nil when not a reply
	// Open is true while a question / escalation card still waits for an answer.
	Open bool
	// Job is the live job of a progress card (nil when unknown).
	Job *Job
}
