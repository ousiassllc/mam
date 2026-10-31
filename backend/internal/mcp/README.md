# mam MCP サーバー: ツールの足し方

画面の操作を MCP ツールにするための基盤。設計は `spec/architecture/mcp.md`、ツール一覧は `spec/api/mcp-tools.md`（ドメインごとに `spec/api/mcp-tools-<ドメイン>.md` を足してリンクする）。

```text
mcp/bind        待ち受け先の規則と mcp.json
mcp/server      Streamable HTTP・ベアラー認証・Origin 検査・Manager（リスナーのライブ切り替え）
mcp/mcpkit      Registrar（Read/Write/Destroy）・エラー写像・Jobs・Files。ツールのパッケージには依存しない
mcp/mcpkit/mcptest   ツールのテスト用の、インメモリ MCP クライアントつきの環境
mcp/tools/<ドメイン>  ツール。Register(r *mcpkit.Registrar, svc *service.Services) を公開する
mcp/toolset     全ツールパッケージの Register を並べる 1 ファイル（all.go）
```

## ツールを足す（共有コードの変更は toolset の 1 行だけ）

1. `backend/internal/mcp/tools/<ドメイン>/` を作る（1 ファイル 300 行・1 ディレクトリ 2000 行の上限。小さいファイルに分ける）。
2. `Register` を書く。ツールは**サービス層を直接呼ぶ**。検証が画面のハンドラにしかない処理は、サービスへ移して両方から呼ぶ。
3. `toolset/all.go` に 1 行足す: `mytools.Register(r, svc)`。
4. `spec/api/mcp-tools-<ドメイン>.md` に表を足し、`mcp-tools.md` の「ドメイン別」に載せる。

```go
package mytools

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func Register(r *mcpkit.Registrar, svc *service.Services) {
	mcpkit.Read(r, "list_projects", "プロジェクトの一覧 / List the Projects.", listProjects(svc.Projects))
	mcpkit.Write(r, "create_thing", "…を作る / Create a thing.", createThing(svc))
	mcpkit.Destroy(r, "delete_thing", "…を削除する / Delete a thing.", deleteThing(svc), mcpkit.Idempotent())
}

type ListProjectsInput struct{} // 引数なしでも構造体にする

type ListProjectsOutput struct {
	Projects []ProjectSummary `json:"projects"` // 一覧は必ずオブジェクトの中に。空でも [] にするなら make() で作る
}

func listProjects(svc *service.ProjectService) mcpkit.Handler[ListProjectsInput, ListProjectsOutput] {
	return func(ctx context.Context, _ ListProjectsInput) (ListProjectsOutput, error) {
		list, err := svc.List(ctx)
		// サービスのエラーはそのまま返す。mcpkit.MapError が code 付きのツールエラーにする
		…
	}
}
```

動く実例は `tools/projects`（読み取り）、`tools/credentials`（変更・削除）、`tools/jobs`（ジョブ）、`tools/files`（ファイル）。

### 規則

- 名前は `snake_case` の `動詞_名詞`（2 語以上）。規約違反と重複は起動時に panic する。`description` は必須、日本語を主に英語でも通じる短さで。
- `Read`（readOnlyHint）／`Write`（destructiveHint=false）／`Destroy`（destructiveHint=true）。冪等なら `mcpkit.Idempotent()`、モデル API を自分で呼ぶなら `mcpkit.OpenWorld()`。
- 入出力は構造体。`json` と `jsonschema:"説明"` のタグがスキーマになり、引数は呼び出し前に検証される。出力は必ずオブジェクト。
- MCP 専用の確認や上限は足さない（画面と同じ扱い）。エラーは `return zero, err` で返す: `ValidationError` は `validation`、`ErrNotFound` は `not_found`、`ErrConflict` / `ErrBusy` は `conflict`、`LockedError` は `locked`、… 未知のエラーは `internal`（文面は出さずログに残る）。`MapError` に無い種類のエラーを足すときは `mcpkit/errors.go` に 1 行足す。
- ID（`project_id` など）は文字列で受ける。ファイルを base64 で受けない・返さない。

## 長い操作: `job_id` を返す

```go
// Register の中で、種別の Probe を 1 度だけ登録する（永続化された状態から進捗と結果を導く。副作用なし）
r.Jobs.RegisterKind("generate_shot", func(ctx context.Context, j mcpkit.JobInfo) (mcpkit.Progress, error) {
	// j.Target は start に渡した対象 ID、j.StartedAt は開始時刻（それ以降に作られたものが結果）
	return mcpkit.Progress{State: mcpkit.JobRunning, Message: "…", Done: 1, Total: 3}, nil
})

type GenerateOutput struct {
	mcpkit.JobRef // job_id と state
}

func generate(r *mcpkit.Registrar, svc *service.Services) mcpkit.Handler[GenerateInput, GenerateOutput] {
	return func(ctx context.Context, in GenerateInput) (GenerateOutput, error) {
		ref, err := r.Jobs.Run(ctx, "generate_shot", in.ShotID, func(ctx context.Context) (string, error) {
			_, err := svc.Generation.GenerateShot(ctx, in.ShotID, in.Instruction)
			return "", err // 永続ジョブ（job テーブル）の ID を返せるなら返す。返せなければ ""
		})
		return GenerateOutput{JobRef: ref}, err
	}
}
```

シグネチャ:

```go
func (j *Jobs) Run(ctx context.Context, kind, target string,
	start func(ctx context.Context) (queueJobID string, err error)) (JobRef, error)
func (j *Jobs) RegisterKind(kind string, probe Probe, opts ...KindOption) // Probe = func(ctx, JobInfo) (Progress, error)
func (j *Jobs) Get(ctx context.Context, id string) (JobStatus, error) // get_job が呼ぶ
type JobRef struct { JobID string; State JobState }             // 出力に埋め込む
type Progress struct { State JobState; Message string; Done, Total int; Result map[string]any; Error string }
```

- `get_job` は基盤にある（足さない）。クライアントは返された `job_id` を渡すだけ。
- `queueJobID` を返すと `job_id` がジョブキューの行の ID になり、`queued` / `running` / `failed`（理由つき）は行から取り、再起動後も使える。返さないと `job_<UUIDv7>`。どちらもジョブの履歴（`mcp_job_history`。30 日・10,000 行）に残るので、再起動後も `get_job` で引ける。Probe が永続化済みの状態ではなくメモリ上の記録だけを読む種別は `r.Jobs.RegisterKind(kind, probe, mcpkit.InMemory())` で登録する（再起動後は履歴の記録で答える。終わりを履歴に残すには `Jobs.Observe` を呼ぶ）。Probe のない種別は `queueJobID` が必須。サービスがジョブを返さないなら、`service` に ID を返すメソッドを足してよい（`orchestrator.Start` は `domain.Job` を返す）。
- `generate_shot`（Shot の生成・Candidates）の Probe は `tools/jobs` にある。Judge・Repair・書き出しなどは、そのツールのパッケージが種別を足す。状態は Shot / Take / Project の永続状態から導く（`StartedAt` より後に作られたものを結果にする）。

## ファイルを渡す・受け取る

```go
// ダウンロード: 種別を 1 度登録し、結果に URL を入れる（Write を使えば書き出しながら送れる）
r.Files.RegisterDownload("take_video", func(ctx context.Context, takeID string) (mcpkit.DownloadFile, error) {
	f, err := svc.Takes.File(ctx, takeID) // サービスのエラー（not_found など）はそのまま返す
	if err != nil {
		return mcpkit.DownloadFile{}, err
	}
	return mcpkit.DownloadFile{Name: "take.mp4", ContentType: "video/mp4", Path: f.Path}, nil
})
link, err := r.Files.DownloadURL(ctx, "take_video", takeID) // mcpkit.Link{URL, ExpiresAt, FileName}。無いなら ErrNotFound

// アップロード: 用途を 1 度登録する。create_upload_ticket（基盤にある）が URL を発行する
r.Files.RegisterUpload(mcpkit.UploadSpec{
	Purpose: "chat_attachment", Description: "…", MaxBytes: service.MaxVideoBytes,
	Params: []mcpkit.UploadParam{{Name: "project_id", Required: true}},
	Handle: func(ctx context.Context, in mcpkit.UploadInput) (any, error) {
		// in.Params は発行時に固定した値（クライアントは変えられない）。in.Upload() は service.Upload
		return map[string]any{"asset_id": id}, svc.Chat.…(ctx, in.Params["project_id"], in.Upload())
	},
})
```

- URL は 15 分・単一リソース・メモリ上（`/mcp-files/<ticket>`）。ダウンロードは繰り返せ、アップロードは 1 回だけ（サービスが拒否したときは使い切らない）。検証は画面と同じ `service/upload.go`。
- 用途 `import_bundle`・`reference_image`、種別 `project_asset` は基盤が登録済み。同じ名前を登録すると panic する。

## テスト

`mcptest.New` がサービス（一時 DB・メモリのキーストア）を作り、`register` で渡したツールを MCP サーバーに載せて、インメモリのクライアントをつなぐ。認証やリスナーは通らない（それは `mcp/server` のテスト）。

```go
env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { projects.Register(r, env.Services) })

var out projects.ListProjectsOutput
env.CallOK("list_projects", map[string]any{}, &out)          // 成功して構造化出力を out に読む
msg := env.CallError("set_credential", map[string]any{…})    // ツールエラーの文面 "<code>: <message>"
env.Registrar.Jobs / env.Registrar.Files                     // ジョブ・ファイルの確認
env.DB, env.Keys, env.AssetsDir, env.Services                // 状態の準備と確認
```

- 生成を伴うテストは `mcptest.New(t, register, mcptest.WithJobQueue)` でジョブキューを足し、`deps` で `service.Deps` を調整する。
- 全ツールをつないだ結合（トークン・Origin・URL）は `mcp/server` の `fixture_test.go` の `newFixture` を使う: 実サーバーを `httptest` で立て、`f.connect(token)` が本物の MCP クライアントをつなぐ。
- 「キーの値を返さない」のような安全の性質は、結果の全文（`json.Marshal(res)`）に対して確かめる。
## 確認

```text
make generate && go build ./... && go vet ./backend/internal/mcp/...
go test -race ./backend/internal/mcp/... ./backend/internal/service/mcp... ./backend/internal/repository/mcptoken/
```
