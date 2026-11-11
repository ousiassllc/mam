package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/secretguard"
)

// ProjectService manages Projects, their Brief, the Confirmation Level and assets.
type ProjectService struct{ *core }

// CreateProjectInput is the first message of a new Project (POST /projects).
type CreateProjectInput struct {
	Content string
	Uploads []Upload
	// BrandTemplateID, when set, copies that Brand Template's constraints into the new Project.
	BrandTemplateID string
}

// Create starts a Project from the user's first message: the Brief is extracted from it (with where each value came from and the recommended Duration / Aspect Ratio of the platform),
// attachments become Assets/References, and the AI answers in the new thread (the Subject question, or the Brief summary to confirm; #1114). A message that carries an API key is
// handled as in the chat: the key is hidden, the message is not read, and its attachments are not taken (the thread says so); the Project is still created.
func (s *ProjectService) Create(ctx context.Context, in CreateProjectInput) (domain.Project, error) {
	content := strings.TrimSpace(in.Content)
	if err := validateMessage(content, len(in.Uploads) > 0); err != nil {
		return domain.Project{}, err
	}
	content, hasKey := secretguard.Redact(content) // an API key is never read by the extractor (an LLM) nor kept in the thread (#1140)
	uploads := in.Uploads
	if hasKey {
		uploads = nil // as in the chat: the message is not read, so its files are not taken (filesNotTakenNotice)
	}
	projectID := domain.NewID()
	stored, err := s.storeUploads(projectID, uploads)
	if err != nil {
		return domain.Project{}, err
	}
	assets := assetsOf(stored)
	var brief domain.Brief
	if !hasKey {
		briefing.Absorb(&brief, s.extractor.Extract(content), content)
	}
	briefing.Recommend(&brief)
	brief.AttachAssets(assets)
	messages := []domain.ChatMessage{userText(content, "", assetIDs(assets))}
	if hasKey {
		messages = secretRefusal(content, len(in.Uploads) > 0)
	}
	messages = assignIDs(append(messages, briefing.FollowUp(brief, domain.ConfirmKeyCheck, true)))
	project := domain.Project{
		ID: projectID, State: domain.StateDraft, Mode: domain.ModeAuto,
		ConfirmationLevel:  domain.ConfirmKeyCheck,
		ProductionTemplate: briefing.TemplateFor(brief.ContentType),
		QualityPolicy:      domain.QualityBalanced,
	}
	err = s.projects.Create(ctx, repository.NewProject{Project: project, Brief: brief, Assets: assets, Messages: withProject(messages, projectID), BrandTemplateID: strings.TrimSpace(in.BrandTemplateID)})
	if err != nil {
		discardUploads(stored)
		if errors.Is(err, repository.ErrTemplateNotFound) {
			return domain.Project{}, invalidTemplate()
		}
		return domain.Project{}, err
	}
	created, err := s.getProject(ctx, projectID)
	if err != nil {
		return domain.Project{}, err
	}
	// A budget stated in the first message ("予算は $20 で") is asked about before anything starts (#1139): the pipeline is kicked only once its card is in the thread.
	if s.askStatedBudget(ctx, created, content) {
		s.kick(ctx, projectID)
	}
	return s.getProject(ctx, projectID)
}

// List returns every Project, newest first.
func (s *ProjectService) List(ctx context.Context) ([]domain.Project, error) {
	return s.projects.List(ctx)
}

// Get returns a Project.
func (s *ProjectService) Get(ctx context.Context, id string) (domain.Project, error) {
	return s.getProject(ctx, id)
}

// BriefView is the Project with its Brief and the Assets the Brief refers to.
type BriefView struct {
	Project domain.Project
	Brief   domain.Brief
	Assets  []domain.Asset // Brief.Assets and the asset references of Brief.References
}

// Brief returns the Creative Brief of the Project (auxiliary view, F-01).
func (s *ProjectService) Brief(ctx context.Context, projectID string) (BriefView, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return BriefView{}, err
	}
	brief, err := s.projects.GetBrief(ctx, projectID)
	if err != nil {
		return BriefView{}, notFound(err)
	}
	assets, err := s.assets.ListByIDs(ctx, projectID, slices.Concat(brief.Assets, brief.References))
	if err != nil {
		return BriefView{}, err
	}
	return BriefView{Project: project, Brief: brief, Assets: assets}, nil
}

// BriefForm is the editable part of the Brief (PUT /projects/:id/brief). Assets and
// video references keep their identity; References (one URL per line) replaces the URL references.
type BriefForm struct {
	Values                map[domain.BriefField]string
	AdditionalInstruction string
	References            string
	// KeepProposals leaves the values the form does not change as they are (recommended or extracted ones stay unconfirmed): a partial
	// update by a client that did not show the user the whole Brief. Saved from the Brief form, the user saw every value, so all of them
	// become the user's (#1114).
	KeepProposals bool
	// Given lists the items the client set explicitly (update_brief): they become the user's even when the value equals the recommended or
	// extracted one. A Subject in Given also settles the open contradiction.
	Given []domain.BriefField
}

// UpdateBrief validates and saves the edited Brief and records the change in the thread.
// On a ValidationError the returned BriefView holds the unsaved input so the form can be
// shown again with the user's values.
func (s *ProjectService) UpdateBrief(ctx context.Context, projectID string, form BriefForm) (BriefView, error) {
	view, err := s.Brief(ctx, projectID)
	if err != nil {
		return BriefView{}, err
	}
	edited := briefing.Clone(view.Brief)
	briefing.ApplyEdit(&edited, form.Values, form.AdditionalInstruction, form.Given, !form.KeepProposals)
	urls, err := ParseReferenceURLs(form.References)
	if err != nil {
		view.Brief = edited
		return view, err
	}
	edited.References = slices.Concat(slices.DeleteFunc(slices.Clone(edited.References), isURL), urls)
	if err := validateBrief(edited); err != nil {
		view.Brief = edited
		return view, err
	}
	note := assistantText("Brief を更新しました。")
	msgs := assignIDs([]domain.ChatMessage{note})
	if err := s.projects.Apply(ctx, repository.Turn{ProjectID: projectID, Messages: msgs, Brief: &edited}); err != nil {
		return BriefView{}, notFound(err)
	}
	s.publishCreated(projectID, msgs)
	s.kick(ctx, projectID)
	view.Brief = edited
	return view, nil
}

func validateBrief(b domain.Brief) error {
	for _, f := range domain.RequiredBriefFields() {
		if problem := briefing.ValueProblem(f, b.Get(f)); problem != "" {
			return invalid(string(f), "%s", problem)
		}
	}
	if utf8.RuneCountInString(b.AdditionalInstruction) > domain.MaxMessageRunes {
		return invalid("additional_instruction", "%d 文字以内で入力してください", domain.MaxMessageRunes)
	}
	for _, r := range b.References {
		if problem := briefing.ReferenceProblem(r); isURL(r) && problem != "" {
			return invalid("references", "%s: %s", problem, r)
		}
	}
	return nil
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// ParseReferenceURLs turns the References textarea (one URL per line) into a list,
// rejecting lines that are not http(s) URLs.
func ParseReferenceURLs(text string) ([]string, error) {
	var urls []string
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !isURL(line) {
			return nil, invalid("references", "http:// または https:// で始まる URL を1行に1件ずつ入力してください")
		}
		urls = append(urls, line)
	}
	return urls, nil
}

// AssetFile is an asset with the absolute path of its file.
type AssetFile struct {
	Asset domain.Asset
	Path  string
}

// Asset locates an uploaded asset of the Project for serving.
func (s *ProjectService) Asset(ctx context.Context, projectID, assetID string) (AssetFile, error) {
	a, err := s.assets.Get(ctx, projectID, assetID)
	if err != nil {
		return AssetFile{}, notFound(err)
	}
	path := s.assetPath(a.FilePath)
	if path == "" {
		return AssetFile{}, ErrNotFound // a stored path outside the assets directory is never served
	}
	return AssetFile{Asset: a, Path: path}, nil
}

func validateMessage(content string, hasAttachments bool) error {
	if content == "" && !hasAttachments {
		return invalid("content", "発言または添付ファイルを入力してください")
	}
	if utf8.RuneCountInString(content) > domain.MaxMessageRunes {
		return invalid("content", "%d 文字以内で入力してください", domain.MaxMessageRunes)
	}
	return nil
}

func assetIDs(assets []domain.Asset) []string {
	ids := make([]string, len(assets))
	for i, a := range assets {
		ids[i] = a.ID
	}
	return ids
}

func withProject(msgs []domain.ChatMessage, projectID string) []domain.ChatMessage {
	for i := range msgs {
		msgs[i].ProjectID = projectID
	}
	return msgs
}

// budgetNotAsked is what the thread says when the confirmation card of the budget stated in the first message could not be posted: the Project exists, nothing was started
// and no ceiling is set.
const budgetNotAsked = "予算の確認カードを出せませんでした。予算はまだ設定していません。生成も始めていません。「予算は $20 で」のようにもう一度伝えてください。"

// askStatedBudget posts the confirmation card of the budget the first message of a new Project states (#1139). It is a second write after the Project was created (the card
// is asked of the stored Project), so a failure must not fail Create: the Project exists and a retry would create a second one. The failure is logged and the thread says
// that no card could be posted; ok is false then, and the caller does not start the pipeline: a paid generation never starts past a budget the user stated and nobody confirmed.
func (s *ProjectService) askStatedBudget(ctx context.Context, project domain.Project, content string) (ok bool) {
	cards, err := s.statedBudgetCards(ctx, project, content)
	if err == nil && len(cards) > 0 {
		cards = assignIDs(withProject(cards, project.ID))
		if err = s.projects.Apply(ctx, repository.Turn{ProjectID: project.ID, Messages: cards}); err == nil {
			s.publishCreated(project.ID, cards)
		}
	}
	if err == nil {
		return true
	}
	s.logger.Error("post the confirmation card of the stated budget failed", "project_id", project.ID, "err", err)
	notice := assignIDs(withProject([]domain.ChatMessage{assistantText(budgetNotAsked)}, project.ID))
	if err := s.projects.Apply(ctx, repository.Turn{ProjectID: project.ID, Messages: notice}); err != nil {
		s.logger.Error("tell that the stated budget was not asked failed", "project_id", project.ID, "err", err)
		return false
	}
	s.publishCreated(project.ID, notice)
	return false
}
