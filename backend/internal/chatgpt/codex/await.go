package codex

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// await follows the events of the turn until it ends.
func (s *Service) await(ctx context.Context, se *appserver.Session, sub *appserver.Sub, threadID, turnID string) (string, error) {
	var (
		deltas  = map[string]*strings.Builder{}
		order   []string // item ids of the agent messages, in order of arrival
		final   string
		hasText bool
		lastErr *protocol.TurnError
	)
	for {
		ev, ok := sub.Next(ctx, se.Done())
		if !ok {
			if ctx.Err() != nil {
				s.interrupt(se, threadID, turnID)
				return "", ctx.Err()
			}
			return "", &Failure{Kind: KindTransient, Message: endMessage(se)}
		}
		switch ev.Method {
		case protocol.NotifAgentDelta:
			var d protocol.AgentDelta
			if json.Unmarshal(ev.Params, &d) == nil {
				b := deltas[d.ItemID]
				if b == nil {
					b = &strings.Builder{}
					deltas[d.ItemID] = b
					order = append(order, d.ItemID)
				}
				b.WriteString(d.Delta)
			}
		case "item/started", protocol.NotifItemCompleted:
			var n protocol.ItemCompleted
			if json.Unmarshal(ev.Params, &n) != nil {
				continue
			}
			if !protocol.IsPlainItem(n.Item.Type) {
				s.interrupt(se, threadID, turnID)
				return "", &Failure{Kind: KindPermanent, Message: "Codex がツール（" + sanitize(n.Item.Type) + "）を使おうとしたため、ターンを中止しました。テキスト生成ではツールは使えません"}
			}
			if ev.Method == protocol.NotifItemCompleted && n.Item.Type == protocol.ItemAgentMessage {
				if n.Item.Phase != nil && *n.Item.Phase == protocol.PhaseFinalAnswer {
					final, hasText = n.Item.Text, true
				} else if !hasText && n.Item.Text != "" {
					final = n.Item.Text // a message with no phase: the answer unless a final answer follows
				}
			}
		case protocol.NotifError:
			var n protocol.ErrorNotification
			if json.Unmarshal(ev.Params, &n) == nil && !n.WillRetry && n.TurnID == turnID {
				e := n.Error
				lastErr = &e
			}
		case protocol.NotifTurnCompleted:
			var n protocol.TurnCompletedNotification
			if json.Unmarshal(ev.Params, &n) != nil || (n.Turn.ID != "" && n.Turn.ID != turnID) {
				continue
			}
			return s.finish(ctx, se, n.Turn, lastErr, final, deltas, order)
		}
	}
}

// finish is the result of a completed turn.
func (s *Service) finish(ctx context.Context, se *appserver.Session, turn protocol.TurnResult, lastErr *protocol.TurnError, final string, deltas map[string]*strings.Builder, order []string) (string, error) {
	switch turn.Status {
	case protocol.TurnCompleted:
		text := strings.TrimSpace(final)
		if text == "" && len(order) > 0 {
			text = strings.TrimSpace(deltas[order[len(order)-1]].String())
		}
		if text == "" {
			return "", &Failure{Kind: KindTransient, Message: "Codex の答えが空でした"}
		}
		return text, nil
	case protocol.TurnFailed:
		e := turn.Error
		if e == nil {
			e = lastErr
		}
		return "", s.turnFailure(ctx, se, e)
	default: // interrupted (by something other than mam) and anything else
		return "", &Failure{Kind: KindTransient, Message: "Codex のターンが中断されました（" + sanitize(turn.Status) + "）"}
	}
}

// turnFailure maps the error of a failed turn by the table of the Issue. For a usage limit it also looks up when the
// limit is lifted.
func (s *Service) turnFailure(ctx context.Context, se *appserver.Session, e *protocol.TurnError) error {
	if e == nil {
		return &Failure{Kind: KindPermanent, Message: "Codex のターンが失敗しました（理由は不明です）"}
	}
	f := &Failure{Kind: kindOf(e.CodexErrorInfo), Message: sanitize(e.Message)}
	if f.Message == "" {
		f.Message = e.CodexErrorInfo.Kind
	}
	if f.Kind == KindUsageLimit {
		f.ResetsAt = s.limitReset(ctx, se)
	}
	return f
}

// limitReset is when the Codex usage limit that was hit is lifted: the latest reset of a window that is used up, else the
// earliest known reset. Zero when it cannot be read.
func (s *Service) limitReset(ctx context.Context, se *appserver.Session) time.Time {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), resetLookupTimeout)
	defer cancel()
	var rl protocol.RateLimitsResult
	if err := se.Call(ctx, protocol.MethodRateLimitsRead, struct{}{}, &rl); err != nil {
		return time.Time{}
	}
	snap := rl.Codex()
	s.setRateLimits(snap)
	var used, any time.Time
	for _, w := range []*protocol.RateLimitWindow{snap.Primary, snap.Secondary} {
		if w == nil || w.ResetsAt == nil {
			continue
		}
		t := time.Unix(*w.ResetsAt, 0).UTC()
		if w.UsedPercent >= 100 && t.After(used) {
			used = t
		}
		if any.IsZero() || t.Before(any) {
			any = t
		}
	}
	if !used.IsZero() {
		return used
	}
	return any
}
