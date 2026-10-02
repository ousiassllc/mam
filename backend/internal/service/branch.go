package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// BranchService makes and shows Branches: other developments of the story that start at a Scene,
// Shot or Frame, each with its own World State and derived tree (F-17, UC-5). The chat thread is
// not split by Branch: a Project has one thread.
type BranchService struct {
	*core
	shots     *repository.GenerationRepository
	continues *ContinueService
	// brands are the Safety / Brand Constraints a Direction is checked against before anything is made.
	brands *repository.BrandRepository
}

// BranchInput is the request to branch at a Shot (POST /shots/:id/branch).
type BranchInput struct {
	// PointType is scene (the Scene of the Shot), shot (after the Shot) or frame; "" is shot.
	PointType domain.BranchPointType
	// OffsetSeconds is the position inside the Shot of a frame Branch Point, in seconds as typed.
	OffsetSeconds string
	// Name is the name of the Branch; "" numbers it ("Branch 2") or, with a Direction, names it after it.
	Name string
	// Direction, when set, has the AI plan and generate the first Shot of the Branch from it.
	Direction string
}

// BranchNode is a Branch of the tree with the numbers the user reads.
type BranchNode struct {
	domain.Branch
	Children []*BranchNode
}

// BranchTree is the Branch tree page: the Branches of the Project by where they hang, and the
// Shots a new Branch can start from.
type BranchTree struct {
	Project  domain.Project
	Roots    []*BranchNode
	Branches []domain.Branch
	Shots    []BranchShotOption
}

// BranchShotOption is a Shot a Branch can be made at, labelled by where it lies.
type BranchShotOption struct {
	ShotID string
	// Label is "Scene 2 / Shot 1", prefixed with the Branch name for the Shot of a Branch.
	Label string
}

// BranchShot is a Shot of the derived tree of a Branch; Scene is the 1-based number of its Scene.
type BranchShot struct {
	Shot  domain.Shot
	Scene int
}

// BranchDetail is the branch_page: the Branch, where it hangs, its own derived tree and its versions.
type BranchDetail struct {
	Project domain.Project
	Branch  domain.Branch
	// Parent is the Branch the Branch Point lies in; nil for the trunk.
	Parent *domain.Branch
	Scenes []domain.Scene
	// Shots are the Shots of the Branch's own Scenes in story order.
	Shots    []BranchShot
	Versions []domain.BranchVersion
	// LatestWorldVersion is the newest version of the Branch's own World State series: the versions
	// the Branch can refer to are 1 to LatestWorldVersion.
	LatestWorldVersion int
	// Started is true when the first Shot of the Branch is being generated.
	Started bool
}

// Tree returns the Branch tree of the Project.
func (s *BranchService) Tree(ctx context.Context, projectID string) (BranchTree, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return BranchTree{}, err
	}
	branches, err := s.branches.List(ctx, projectID)
	if err != nil {
		return BranchTree{}, err
	}
	shots, err := s.shots.ListAllShots(ctx, projectID)
	if err != nil {
		return BranchTree{}, err
	}
	names := make(map[string]string, len(branches))
	for _, b := range branches {
		names[b.ID] = b.Name
	}
	tree := BranchTree{Project: project, Roots: buildTree(branches), Branches: branches}
	for _, sh := range shots {
		label := fmt.Sprintf("Scene %d / Shot %d", sh.Scene.OrderIndex+1, sh.Shot.OrderIndex+1)
		if sh.Scene.BranchID != "" {
			label = names[sh.Scene.BranchID] + ": " + label
		}
		tree.Shots = append(tree.Shots, BranchShotOption{ShotID: sh.Shot.ID, Label: label})
	}
	return tree, nil
}

// buildTree nests the Branches (oldest first) under the Branch their Branch Point lies in.
func buildTree(branches []domain.Branch) []*BranchNode {
	nodes := make(map[string]*BranchNode, len(branches))
	for _, b := range branches {
		nodes[b.ID] = &BranchNode{Branch: b}
	}
	var roots []*BranchNode
	for _, b := range branches {
		n := nodes[b.ID]
		if parent, ok := nodes[b.ParentID]; ok && b.ParentID != "" {
			parent.Children = append(parent.Children, n)
			continue
		}
		roots = append(roots, n)
	}
	return roots
}

// Detail returns the Branch page.
func (s *BranchService) Detail(ctx context.Context, branchID string) (BranchDetail, error) {
	branch, err := s.branches.Get(ctx, branchID)
	if err != nil {
		return BranchDetail{}, notFound(err)
	}
	return s.detail(ctx, branch)
}

func (s *BranchService) detail(ctx context.Context, branch domain.Branch) (BranchDetail, error) {
	project, err := s.getProject(ctx, branch.ProjectID)
	if err != nil {
		return BranchDetail{}, err
	}
	scenes, err := s.branches.Scenes(ctx, branch.ID)
	if err != nil {
		return BranchDetail{}, err
	}
	all, err := s.shots.ListAllShots(ctx, branch.ProjectID)
	if err != nil {
		return BranchDetail{}, err
	}
	versions, err := s.branches.Versions(ctx, branch.ID)
	if err != nil {
		return BranchDetail{}, err
	}
	latest, err := s.branches.LatestWorldVersion(ctx, branch.ID)
	if err != nil {
		return BranchDetail{}, err
	}
	d := BranchDetail{Project: project, Branch: branch, Scenes: scenes, Versions: versions, LatestWorldVersion: latest}
	for _, sh := range all {
		if sh.Scene.BranchID == branch.ID {
			d.Shots = append(d.Shots, BranchShot{Shot: sh.Shot, Scene: sh.Scene.OrderIndex + 1})
		}
	}
	if branch.ParentID != "" {
		parent, err := s.branches.Get(ctx, branch.ParentID)
		if err != nil {
			return BranchDetail{}, notFound(err)
		}
		d.Parent = &parent
	}
	return d, nil
}

// Create saves the Branch Point at the Shot and snapshots the World State there (POST
// /shots/:id/branch). With a Direction the AI also plans the first Shot of the Branch and starts
// generating it; a Direction needs generation to be configured (ErrUnavailable otherwise, before
// anything is made), and one with a forbidden expression of a LOCK Brand constraint is
// ErrBrandViolation, also before anything is made. An unknown Shot is ErrNotFound; an unusable type,
// offset or name is a ValidationError.
func (s *BranchService) Create(ctx context.Context, shotID string, in BranchInput) (BranchDetail, error) {
	return s.create(ctx, shotID, in, false)
}

// create is Create; aiChoice has the AI plan and generate the first Shot even without a Direction
// (the development is left to it).
func (s *BranchService) create(ctx context.Context, shotID string, in BranchInput, aiChoice bool) (BranchDetail, error) {
	ref, err := s.shots.GetShot(ctx, shotID)
	if err != nil {
		return BranchDetail{}, notFound(err)
	}
	spec, err := s.newBranch(ctx, ref, in)
	if err != nil {
		return BranchDetail{}, err
	}
	direction := strings.TrimSpace(in.Direction)
	generate := direction != "" || aiChoice
	if generate && !s.canContinue() {
		return BranchDetail{}, ErrUnavailable
	}
	if err := s.checkDirections(ctx, ref.ProjectID, direction); err != nil {
		return BranchDetail{}, err
	}
	branch, err := s.branches.Create(ctx, spec)
	if err != nil {
		return BranchDetail{}, notFound(err)
	}
	started := false
	if generate {
		if err := s.continueBranch(ctx, ref, branch, direction); err != nil {
			return BranchDetail{}, err
		}
		started = true
	}
	d, err := s.detail(ctx, branch)
	d.Started = started
	return d, err
}

// newBranch validates the request and turns it into what the repository stores.
func (s *BranchService) newBranch(ctx context.Context, ref repository.ShotRef, in BranchInput) (repository.NewBranch, error) {
	typ := in.PointType
	if typ == "" {
		typ = domain.BranchAtShot
	}
	if !typ.Valid() {
		return repository.NewBranch{}, invalid("point_type", "分岐点の種類は scene / shot / frame のいずれかです")
	}
	spec := repository.NewBranch{ShotID: ref.Shot.ID, PointType: typ}
	if typ == domain.BranchAtFrame {
		offset, err := strconv.ParseFloat(strings.TrimSpace(in.OffsetSeconds), 64)
		if err != nil || math.IsNaN(offset) || math.IsInf(offset, 0) || offset < 0 || (ref.Shot.Duration > 0 && offset > ref.Shot.Duration) {
			return repository.NewBranch{}, invalid("offset_seconds", "分岐する位置を 0 秒以上、Shot の長さ以内の秒数で指定してください")
		}
		spec.OffsetSeconds = offset
	}
	name := strings.TrimSpace(in.Name)
	switch {
	case name != "":
		clean, ok := domain.CleanBranchName(name)
		if !ok {
			return repository.NewBranch{}, invalid("name", "Branch の名前は %d 文字以内で入力してください", domain.MaxBranchNameRunes)
		}
		spec.Name = clean
	case strings.TrimSpace(in.Direction) != "":
		spec.Name = domain.TruncateBranchName(in.Direction)
	default:
		existing, err := s.branches.List(ctx, ref.ProjectID)
		if err != nil {
			return repository.NewBranch{}, err
		}
		spec.Name = fmt.Sprintf("Branch %d", len(existing)+1)
	}
	return spec, nil
}

// canContinue reports whether the AI can plan and generate Shots (the Continue Agent and the
// generation queue are configured).
func (s *BranchService) canContinue() bool {
	return s.continues != nil && s.continues.agent != nil && s.continues.generation.gen != nil
}

// continueBranch has the AI plan the first Shot of the Branch from direction, adds it to the
// Branch's own tree (a Scene that inherits the Scene of the Branch Point unless the AI moves the
// story on) and starts generating it from the Branch's World State. When the Branch was made but
// this fails, the Branch stays and the error says so.
func (s *BranchService) continueBranch(ctx context.Context, ref repository.ShotRef, branch domain.Branch, direction string) error {
	in, err := s.continues.input(ctx, ref)
	if err != nil {
		return err
	}
	in.World, err = s.shots.WorldContext(ctx, ref.ProjectID, branch.ID)
	if err != nil {
		return err
	}
	plan, err := s.continues.agent.Plan(ctx, in, direction)
	if err != nil {
		return branchMadeError(aiError(err))
	}
	scene := domain.Scene{
		BranchID: branch.ID, Title: ref.Scene.Title, Location: ref.Scene.Location, Time: ref.Scene.Time, Objective: ref.Scene.Objective,
	}
	if plan.NewScene {
		scene.Title, scene.Location, scene.Time, scene.Objective = plan.Scene.Title, plan.Scene.Location, plan.Scene.Time, plan.Scene.Objective
	}
	shot, err := s.plans.AppendContinuation(ctx, ref.ProjectID, &scene, domain.Shot{Objective: plan.Shot.Objective, Duration: plan.Shot.DurationSeconds})
	if err != nil {
		return notFound(err)
	}
	if _, err := s.continues.generation.gen.Start(ctx, shot.ID, direction); err != nil {
		return branchMadeError(mapGenerationError(err))
	}
	s.recordIntervention(ctx, domain.Intervention{ShotID: shot.ID, Kind: domain.InterventionGenerate}) // the user asked for the Branch's first Shot
	return nil
}

// branchMadeError tells the user that the Branch exists although starting its first Shot failed.
func branchMadeError(err error) error {
	var ai *AIError
	if errors.As(err, &ai) {
		return &AIError{Reason: "Branch は作成しましたが、最初の Shot を計画できませんでした: " + ai.Reason, Err: ai.Err}
	}
	return err
}

// Update renames the Branch and/or points it at another World State version of its own series
// (PUT /branches/:id); the change is appended to branch_version. Neither given is a ValidationError.
func (s *BranchService) Update(ctx context.Context, branchID string, name, worldStateVersion *string) (BranchDetail, error) {
	if name == nil && worldStateVersion == nil {
		return BranchDetail{}, invalid("", "変更する名前か World State のバージョンを指定してください")
	}
	var change repository.BranchChange
	if worldStateVersion != nil {
		n, err := strconv.Atoi(strings.TrimSpace(*worldStateVersion))
		if err != nil || n < 1 {
			return BranchDetail{}, invalid("world_state_version", "World State のバージョンは 1 以上の整数で指定してください")
		}
		change.WorldStateVersion = &n
	}
	if name != nil {
		clean, ok := domain.CleanBranchName(*name)
		if !ok {
			return BranchDetail{}, invalid("name", "Branch の名前は %d 文字以内で入力してください", domain.MaxBranchNameRunes)
		}
		change.Name = &clean
	}
	branch, err := s.branches.Update(ctx, branchID, change)
	switch {
	case errors.Is(err, repository.ErrVersionNotFound):
		return BranchDetail{}, invalid("world_state_version", "この Branch にそのバージョンの World State はありません")
	case err != nil:
		return BranchDetail{}, notFound(err)
	}
	return s.detail(ctx, branch)
}

// Fork makes count Branches at the Shot for a direction of the chat ("このカットから別展開を3つ作って")
// and reports what happened, one line per Branch. directions are the developments the user named;
// the AI proposes the rest (the NEXT candidates of Continue). With generation configured every
// Branch gets its first Shot planned and generated (one the AI could propose no development for is
// left to its own choice); without it the Branches are made empty. count is capped at domain.MaxBranchesPerRequest.
func (s *BranchService) Fork(ctx context.Context, ref repository.ShotRef, typ domain.BranchPointType, count int, directions []string) ([]string, error) {
	count = min(max(count, 1), domain.MaxBranchesPerRequest)
	if !typ.Valid() {
		typ = domain.BranchAtShot
	}
	directions = append([]string{}, directions[:min(len(directions), count)]...)
	if len(directions) < count && s.canContinue() {
		in, err := s.continues.input(ctx, ref)
		if err != nil {
			return nil, err
		}
		proposal, err := s.continues.agent.Propose(ctx, in)
		if err != nil {
			return nil, aiError(err)
		}
		for _, c := range proposal.Candidates {
			if len(directions) == count {
				break
			}
			directions = append(directions, c)
		}
	}
	if s.canContinue() {
		// the directions the AI proposed too: a forbidden one stops the request before the first Branch
		if err := s.checkDirections(ctx, ref.ProjectID, directions...); err != nil {
			return nil, err
		}
	}
	var lines []string
	for i := range count {
		in := BranchInput{PointType: typ}
		if i < len(directions) {
			if s.canContinue() {
				in.Direction = directions[i]
			} else {
				in.Name = directions[i] // nothing can be generated: the direction only names the Branch
			}
		}
		d, err := s.create(ctx, ref.Shot.ID, in, s.canContinue())
		if err != nil {
			line, fatal := forkFailure(err)
			if fatal != nil {
				return lines, fatal
			}
			lines = append(lines, line)
			continue
		}
		lines = append(lines, forkLine(d))
	}
	return lines, nil
}

// checkDirections returns ErrBrandViolation when a direction contains a forbidden expression of a
// LOCK Brand constraint (spec §23: before anything is generated or made).
func (s *BranchService) checkDirections(ctx context.Context, projectID string, directions ...string) error {
	return checkBrandTexts(ctx, s.brands, projectID, directions...)
}

func forkLine(d BranchDetail) string {
	line := fmt.Sprintf("Branch「%s」を %s から作成しました。", d.Branch.Name, d.Branch.PointText())
	if d.Started {
		return line + "最初の Shot の生成を始めました。"
	}
	return line
}

// forkFailure turns an error of one Branch into a line for the thread, or returns it as fatal when
// the user cannot act on it.
func forkFailure(err error) (string, error) {
	var ai *AIError
	switch {
	case errors.As(err, &ai):
		return ai.Reason, nil
	case errors.Is(err, ErrBudgetExceeded):
		return "予算上限を超えるため、Branch の最初の Shot は生成しませんでした（Branch は作成済みです）。", nil
	case errors.Is(err, ErrBusy), errors.Is(err, ErrUnavailable):
		return "Branch の最初の Shot を生成できませんでした（Branch は作成済みです）。", nil
	}
	return "", err
}
