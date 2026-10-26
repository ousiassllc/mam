package chat

import (
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Project is the state of the Project next to its thread.
type Project struct {
	ID                string `json:"id"`
	State             string `json:"state" jsonschema:"Core State Machine の状態（DRAFT, PLANNING, AWAITING_APPROVAL, GENERATING ...）"`
	Mode              string `json:"mode" jsonschema:"auto or director"`
	ConfirmationLevel string `json:"confirmation_level" jsonschema:"full_auto, key_check or custom_check"`
}

// Attachment is a file a message carries. Download it with get_asset_download_url (project_id + asset_id).
type Attachment struct {
	AssetID   string `json:"asset_id"`
	Type      string `json:"type" jsonschema:"image or video"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
}

// TakeCard is the body of a take card. The video and the poster of the screen are same-origin paths of
// the browser and are not part of the output: the Take is take_id.
type TakeCard struct {
	TakeID  string   `json:"take_id"`
	ShotID  string   `json:"shot_id,omitempty"`
	Title   string   `json:"title"`
	Badge   string   `json:"badge,omitempty" jsonschema:"AI Pick など"`
	Score   *float64 `json:"score,omitempty" jsonschema:"Judge の総合スコア（0-1）"`
	Summary string   `json:"summary,omitempty"`
}

// Job is the background job behind a progress card.
type Job struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status" jsonschema:"queued, running, succeeded or failed"`
	Error  string `json:"error,omitempty"`
}

// Message is one entry of the thread.
type Message struct {
	ID               string       `json:"id" jsonschema:"時系列順に増える。list_chat_messages の after に渡せる"`
	Role             string       `json:"role" jsonschema:"user or assistant"`
	Kind             string       `json:"kind" jsonschema:"text, question, approval, keyframe, take, progress or escalation"`
	Text             string       `json:"text" jsonschema:"本文。question / escalation は質問・相談の文面、take はタイトル、approval は確認する内容の要約"`
	Options          []string     `json:"options,omitempty" jsonschema:"question / escalation のクイック回答（そのまま post_chat_message の content にできる）"`
	NonBlocking      bool         `json:"non_blocking,omitempty" jsonschema:"escalation: プロジェクトを止めていない相談"`
	ReplyToMessageID string       `json:"reply_to_message_id,omitempty"`
	RefType          string       `json:"ref_type,omitempty" jsonschema:"カードが指す対象の種類（brief_field, take, job, shot, script_version ...）"`
	RefID            string       `json:"ref_id,omitempty" jsonschema:"ref_type の対象の ID。question(brief_field) では聞いている Brief の項目名、keyframe では asset_id"`
	ApprovalStatus   string       `json:"approval_status,omitempty" jsonschema:"approval: pending, approved or changes_requested"`
	Open             bool         `json:"open" jsonschema:"question / escalation が回答を待っている"`
	Pending          bool         `json:"pending" jsonschema:"このカードがまだ変わりうる（回答待ち・未承認・ジョブ実行中）。get_chat_message で最新を取り直す"`
	Attachments      []Attachment `json:"attachments,omitempty"`
	Take             *TakeCard    `json:"take,omitempty"`
	Job              *Job         `json:"job,omitempty" jsonschema:"progress: 紐づくジョブの今の状態"`
	CreatedAt        time.Time    `json:"created_at"`
}

func projectOf(p domain.Project) Project {
	return Project{ID: p.ID, State: string(p.State), Mode: string(p.Mode), ConfirmationLevel: string(p.ConfirmationLevel)}
}

func messagesOf(list []domain.ThreadMessage) []Message {
	out := make([]Message, 0, len(list))
	for _, m := range list {
		out = append(out, messageOf(m))
	}
	return out
}

func messageOf(m domain.ThreadMessage) Message {
	out := Message{
		ID: m.ID, Role: string(m.Role), Kind: string(m.Kind), Text: m.Content,
		ReplyToMessageID: m.ReplyToMessageID, RefType: m.RefType, RefID: m.RefID,
		ApprovalStatus: string(m.ApprovalStatus), Open: m.Open, Pending: m.Pending(), CreatedAt: m.CreatedAt,
	}
	switch m.Kind {
	case domain.KindQuestion, domain.KindEscalation:
		body := domain.DecodeCardBody(m.Content)
		out.Text, out.Options, out.NonBlocking = body.Text, body.Options, body.NonBlocking
	case domain.KindTake:
		body := domain.DecodeTakeCardBody(m.Content)
		out.Text = body.Title
		out.Take = &TakeCard{TakeID: m.RefID, ShotID: body.ShotID, Title: body.Title, Badge: body.Badge, Score: body.Score, Summary: body.Summary}
	}
	for _, a := range m.Attachments {
		out.Attachments = append(out.Attachments, Attachment{AssetID: a.ID, Type: string(a.Type), MimeType: a.MimeType, SizeBytes: a.SizeBytes})
	}
	if j := m.Job; j != nil {
		out.Job = &Job{ID: j.ID, Kind: string(j.Kind), Status: string(j.Status), Error: j.Error}
	}
	return out
}
