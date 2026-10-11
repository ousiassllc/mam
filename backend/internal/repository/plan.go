package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// PlanRepository persists what the planning stages produce: script versions, Scenes and
// Shots (spec/architecture/er.md §4). All writes go through DB.Write.
type PlanRepository struct {
	db  *DB
	now func() time.Time
}

// NewPlanRepository returns a PlanRepository backed by db.
func NewPlanRepository(db *DB) *PlanRepository {
	return &PlanRepository{db: db, now: time.Now}
}

// SaveScript appends the next script version (version_number = latest + 1) of the Project.
func (r *PlanRepository) SaveScript(ctx context.Context, projectID string, doc domain.ScriptDoc) (domain.ScriptVersion, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return domain.ScriptVersion{}, fmt.Errorf("marshal script: %w", err)
	}
	v := domain.ScriptVersion{ID: domain.NewID(), ProjectID: projectID, Doc: doc, CreatedAt: r.now().UTC()}
	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := projectExists(ctx, tx, projectID); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(version_number), 0) + 1 FROM script_version WHERE project_id = ?`, projectID).Scan(&v.VersionNumber); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO script_version (id, project_id, version_number, content, created_at) VALUES (?, ?, ?, ?, ?)`,
			v.ID, projectID, v.VersionNumber, string(raw), formatTime(v.CreatedAt))
		return err
	})
	if err != nil {
		return domain.ScriptVersion{}, fmt.Errorf("save script: %w", err)
	}
	return v, nil
}

// LatestScript returns the newest script version of the Project or ErrNotFound.
func (r *PlanRepository) LatestScript(ctx context.Context, projectID string) (domain.ScriptVersion, error) {
	var (
		v       domain.ScriptVersion
		content string
		created string
	)
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT id, project_id, version_number, content, created_at FROM script_version
		 WHERE project_id = ? ORDER BY version_number DESC LIMIT 1`, projectID).
		Scan(&v.ID, &v.ProjectID, &v.VersionNumber, &content, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ScriptVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.ScriptVersion{}, fmt.Errorf("get latest script: %w", err)
	}
	if err := json.Unmarshal([]byte(content), &v.Doc); err != nil {
		return domain.ScriptVersion{}, fmt.Errorf("decode script: %w", err)
	}
	if v.CreatedAt, err = ParseTime(created); err != nil {
		return domain.ScriptVersion{}, err
	}
	return v, nil
}

func projectExists(ctx context.Context, tx *sql.Tx, projectID string) error {
	var one int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM project WHERE id = ?`, projectID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// SavePlan stores the Scene / Shot plan of the Project with the storyboard frame of every
// Shot in one transaction. It returns ErrConflict when the Project already has Scenes: a plan
// is never written over existing Scenes, which own Shots and Takes. Locations named by the plan
// are reused by name or created.
func (r *PlanRepository) SavePlan(ctx context.Context, projectID string, plan domain.ScenePlan, sb domain.Storyboard) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := projectExists(ctx, tx, projectID); err != nil {
			return err
		}
		var scenes int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scene WHERE project_id = ? AND branch_id IS NULL`, projectID).Scan(&scenes); err != nil {
			return err
		}
		if scenes > 0 {
			return ErrConflict
		}
		locations := map[string]string{}
		for i, ps := range plan.Scenes {
			locationID, err := locationFor(ctx, tx, projectID, "", ps.Location, locations)
			if err != nil {
				return err
			}
			sceneID := domain.NewID()
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO scene (id, project_id, order_index, title, location_id, time, objective, state) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				sceneID, projectID, i, ps.Title, nullIfEmpty(locationID), nullIfEmpty(ps.Time), nullIfEmpty(ps.Objective), domain.ShotPlanning); err != nil {
				return err
			}
			for j, sh := range ps.Shots {
				var frame *domain.ShotStoryboard
				if i < len(sb.Shots) && j < len(sb.Shots[i]) {
					frame = &sb.Shots[i][j]
				}
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO shot (id, scene_id, order_index, objective, duration_seconds, shot_spec, state) VALUES (?, ?, ?, ?, ?, ?, ?)`,
					domain.NewID(), sceneID, j, sh.Objective, sh.DurationSeconds, string(domain.EncodeShotSpec(frame)), domain.ShotPlanning); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save plan: %w", err)
	}
	return nil
}

// locationFor returns the ID of the Location of the Project (of its Branch when branchID is set, ""
// is the trunk) with that name, creating it when missing. cache holds the IDs resolved earlier in
// the transaction. "" means no Location.
func locationFor(ctx context.Context, tx *sql.Tx, projectID, branchID, name string, cache map[string]string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	if id, ok := cache[name]; ok {
		return id, nil
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM location WHERE project_id = ? AND branch_id IS ? AND name = ? ORDER BY id LIMIT 1`,
		projectID, nullIfEmpty(branchID), name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		id = domain.NewID()
		_, err = tx.ExecContext(ctx, `INSERT INTO location (id, project_id, branch_id, name) VALUES (?, ?, ?, ?)`, id, projectID, nullIfEmpty(branchID), name)
	}
	if err != nil {
		return "", err
	}
	cache[name] = id
	return id, nil
}

// HasScenes reports whether the trunk of the Project has at least one Scene.
func (r *PlanRepository) HasScenes(ctx context.Context, projectID string) (bool, error) {
	var n int
	if err := r.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM scene WHERE project_id = ? AND branch_id IS NULL`, projectID).Scan(&n); err != nil {
		return false, fmt.Errorf("count scenes: %w", err)
	}
	return n > 0, nil
}

const sceneSelect = `
SELECT s.id, s.project_id, COALESCE(s.branch_id, ''), s.order_index, s.title, COALESCE(s.location_id, ''), COALESCE(l.name, ''),
       COALESCE(s.time, ''), COALESCE(s.objective, ''), s.state
FROM scene s LEFT JOIN location l ON l.id = s.location_id`

func scanScene(row rowScanner) (domain.Scene, error) {
	var s domain.Scene
	err := row.Scan(&s.ID, &s.ProjectID, &s.BranchID, &s.OrderIndex, &s.Title, &s.LocationID, &s.Location, &s.Time, &s.Objective, &s.State)
	return s, err
}

// ListScenes returns the Scenes of the trunk of the Project in order (the Scenes of a Branch are
// listed by BranchRepository.Scenes).
func (r *PlanRepository) ListScenes(ctx context.Context, projectID string) ([]domain.Scene, error) {
	rows, err := r.db.Reader().QueryContext(ctx, sceneSelect+` WHERE s.project_id = ? AND s.branch_id IS NULL ORDER BY s.order_index`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list scenes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scenes []domain.Scene
	for rows.Next() {
		s, err := scanScene(rows)
		if err != nil {
			return nil, fmt.Errorf("scan scene: %w", err)
		}
		scenes = append(scenes, s)
	}
	return scenes, rows.Err()
}

// GetScene returns a Scene of the Project or ErrNotFound.
func (r *PlanRepository) GetScene(ctx context.Context, projectID, sceneID string) (domain.Scene, error) {
	s, err := scanScene(r.db.Reader().QueryRowContext(ctx, sceneSelect+` WHERE s.project_id = ? AND s.id = ?`, projectID, sceneID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Scene{}, ErrNotFound
	}
	if err != nil {
		return domain.Scene{}, fmt.Errorf("get scene: %w", err)
	}
	return s, nil
}

// GetSceneByID returns a Scene by its ID alone or ErrNotFound.
func (r *PlanRepository) GetSceneByID(ctx context.Context, sceneID string) (domain.Scene, error) {
	s, err := scanScene(r.db.Reader().QueryRowContext(ctx, sceneSelect+` WHERE s.id = ?`, sceneID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Scene{}, ErrNotFound
	}
	if err != nil {
		return domain.Scene{}, fmt.Errorf("get scene: %w", err)
	}
	return s, nil
}

// CreateScene appends a Scene at the end of the trunk of the Project, or of its Branch when
// s.BranchID is set (F-04). The Location, when named, is reused by name or created. It returns
// ErrNotFound when the Project does not exist.
func (r *PlanRepository) CreateScene(ctx context.Context, s domain.Scene) (domain.Scene, error) {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error { return insertScene(ctx, tx, &s) })
	if err != nil {
		return domain.Scene{}, fmt.Errorf("create scene: %w", err)
	}
	return s, nil
}

func insertScene(ctx context.Context, tx *sql.Tx, s *domain.Scene) error {
	s.ID = domain.NewID()
	s.State = domain.ShotPlanning
	if err := projectExists(ctx, tx, s.ProjectID); err != nil {
		return err
	}
	locationID, err := locationFor(ctx, tx, s.ProjectID, s.BranchID, s.Location, map[string]string{})
	if err != nil {
		return err
	}
	s.LocationID = locationID
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(order_index), -1) + 1 FROM scene WHERE project_id = ? AND branch_id IS ?`, s.ProjectID, nullIfEmpty(s.BranchID)).Scan(&s.OrderIndex); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO scene (id, project_id, branch_id, order_index, title, location_id, time, objective, state) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.ProjectID, nullIfEmpty(s.BranchID), s.OrderIndex, s.Title, nullIfEmpty(s.LocationID), nullIfEmpty(s.Time), nullIfEmpty(s.Objective), s.State)
	return err
}

// CreateShot appends a Shot at the end of the Scene (F-04). It returns ErrNotFound when the Scene does not exist.
func (r *PlanRepository) CreateShot(ctx context.Context, sh domain.Shot) (domain.Shot, error) {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error { return insertShot(ctx, tx, &sh) })
	if err != nil {
		return domain.Shot{}, fmt.Errorf("create shot: %w", err)
	}
	return sh, nil
}

func insertShot(ctx context.Context, tx *sql.Tx, sh *domain.Shot) error {
	sh.ID = domain.NewID()
	sh.State = domain.ShotPlanning
	if len(sh.ShotSpec) == 0 {
		sh.ShotSpec = domain.EncodeShotSpec(nil)
	}
	var one int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM scene WHERE id = ?`, sh.SceneID).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(order_index), -1) + 1 FROM shot WHERE scene_id = ?`, sh.SceneID).Scan(&sh.OrderIndex); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO shot (id, scene_id, order_index, objective, duration_seconds, shot_spec, state) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sh.ID, sh.SceneID, sh.OrderIndex, sh.Objective, sh.Duration, string(sh.ShotSpec), sh.State)
	return err
}

// AppendContinuation adds the Shot Continue planned (F-16) in one transaction. With newScene it
// first appends that Scene at the end of the Project and puts the Shot in it; otherwise the
// Shot goes to the end of the Scene sh.SceneID. It returns the stored Shot, or ErrNotFound when
// the Project or the Scene does not exist.
func (r *PlanRepository) AppendContinuation(ctx context.Context, projectID string, newScene *domain.Scene, sh domain.Shot) (domain.Shot, error) {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if newScene != nil {
			scene := *newScene
			scene.ProjectID = projectID
			if err := insertScene(ctx, tx, &scene); err != nil {
				return err
			}
			sh.SceneID = scene.ID
		}
		return insertShot(ctx, tx, &sh)
	})
	if err != nil {
		return domain.Shot{}, fmt.Errorf("append continuation: %w", err)
	}
	return sh, nil
}

// RemoveShots deletes Shots that were added for a Reshoot round whose generation never started.
// A Shot that is no longer PLANNING is left alone (its job is already running). sceneIDs are the
// Scenes that request created for them: the ones left without a Shot are deleted too.
func (r *PlanRepository) RemoveShots(ctx context.Context, ids, sceneIDs []string) error {
	if len(ids) == 0 {
		return nil
	}
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `DELETE FROM shot WHERE id = ? AND state = ?`, id, domain.ShotPlanning); err != nil {
				return err
			}
		}
		for _, id := range sceneIDs {
			if _, err := tx.ExecContext(ctx, `DELETE FROM scene WHERE id = ? AND NOT EXISTS (SELECT 1 FROM shot WHERE scene_id = scene.id)`, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("remove shots: %w", err)
	}
	return nil
}

// ListShots returns the Shots of the Scene in order.
func (r *PlanRepository) ListShots(ctx context.Context, sceneID string) ([]domain.Shot, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT id, scene_id, order_index, COALESCE(objective, ''), COALESCE(duration_seconds, 0), shot_spec, state, COALESCE(selected_take_id, '')
		 FROM shot WHERE scene_id = ? ORDER BY order_index`, sceneID)
	if err != nil {
		return nil, fmt.Errorf("list shots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var shots []domain.Shot
	for rows.Next() {
		var (
			sh   domain.Shot
			spec string
		)
		if err := rows.Scan(&sh.ID, &sh.SceneID, &sh.OrderIndex, &sh.Objective, &sh.Duration, &spec, &sh.State, &sh.SelectedTakeID); err != nil {
			return nil, fmt.Errorf("scan shot: %w", err)
		}
		sh.ShotSpec = json.RawMessage(spec)
		shots = append(shots, sh)
	}
	return shots, rows.Err()
}

// ListTakes returns the Takes of the Scene's Shots (through their prompt versions), oldest
// first, grouped by Shot ID.
func (r *PlanRepository) ListTakes(ctx context.Context, sceneID string) (map[string][]domain.Take, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT t.id, pv.shot_id, t.prompt_version_id, t.model_name, COALESCE(t.file_path, ''), t.status, t.created_at
		 FROM take t
		 JOIN prompt_version pv ON pv.id = t.prompt_version_id
		 JOIN shot sh ON sh.id = pv.shot_id
		 WHERE sh.scene_id = ? ORDER BY t.created_at, t.id`, sceneID)
	if err != nil {
		return nil, fmt.Errorf("list takes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	takes := map[string][]domain.Take{}
	for rows.Next() {
		var (
			t       domain.Take
			created string
		)
		if err := rows.Scan(&t.ID, &t.ShotID, &t.PromptVersionID, &t.ModelName, &t.FilePath, &t.Status, &created); err != nil {
			return nil, fmt.Errorf("scan take: %w", err)
		}
		if t.CreatedAt, err = ParseTime(created); err != nil {
			return nil, err
		}
		takes[t.ShotID] = append(takes[t.ShotID], t)
	}
	return takes, rows.Err()
}

// ShotCounts returns the number of Shots of every Scene of the Project, by Scene ID.
func (r *PlanRepository) ShotCounts(ctx context.Context, projectID string) (map[string]int, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT sh.scene_id, COUNT(*) FROM shot sh JOIN scene s ON s.id = sh.scene_id WHERE s.project_id = ? GROUP BY sh.scene_id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("count shots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	counts := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan shot count: %w", err)
		}
		counts[id] = n
	}
	return counts, rows.Err()
}
