# 生成テスト: Model Adapter の直接呼び出しとシード

Issue #835。[model-adapters.md](../model-adapters.md)（§9 の実装）に足す、生成テスト向けの追加。`model-adapters.md` は行数が多いため、この追加は別ファイルに書く。実装は `backend/internal/agent/modeladapter`（`ImageRequest`・`VideoRequest`・`Adapter.Unavailable`・`Adapter.Snapshot`・`Call`）と、下位パッケージ `modeladapter/direct`・`modeladapter/gentestcfg`。

## 1. 直接呼び出し（`direct.Image` / `direct.Video`）

通常の `Adapter.GenerateImage`・`GenerateVideo` は、用途から**モデルを選び**、失敗するとフォールバックの連鎖で別のモデルへ切り替える。比較のための生成では、落ちたモデルを別のモデルに置き換えると比較にならないので、**モデルを指定して呼び、代替しない**入口を足した。

- `direct.Image(ctx, adapter, model, ImageRequest)` / `direct.Video(ctx, adapter, VideoTarget, VideoRequest)`。指定のモデルのバックエンドが無ければ `ErrNoBackend`。結果の `Model` は指定したモデルで、`FellBack` は常に false。
- **プロンプトはそのまま送る**（補正しない）。ネガティブの欄が無いモデルへの `NegativePrompt` は捨てる（呼び出し側が先に「Avoid: …」をプロンプトに書き足す。`direct.Fold` = `shotprompt.AvoidSentence` と同じ文）。シードを受け付けないモデルへのシードも捨てる。
- **入力はモデルの能力で検査して拒否する**: 参照画像を使えないモデルに画像があれば `ErrInputsUnsupported`、最初のフレームと自由な参照の併用・最後のフレームだけ・参照が上限超えは `ErrInputsConflict`、モデルが作らない解像度は `ErrResolution`。いずれもベンダーを呼ぶ前なので課金されない。動画の長さは `VideoCapabilities.SnapDuration` で作れる長さに丸める。Runway Gen-4.5 の 1:1 は最初のフレームがあるときだけ（`direct.AspectProblem`。ベンダーは受け付けない。通常の Shot の経路は代替へ切り替えるが、直接呼び出しは失敗する）。生成テストは見積りの時点で警告する。
- **レジリエンスは共有**: `modeladapter.Call[T](ctx, adapter, vendor, clock, model, fn)`（`call[T]` を公開したもの）を通るので、ベンダーごとのサーキットブレーカー・同時実行数（`resilience.Limiter`）・再試行・タイムアウトは通常の呼び出しと同じ。コスト台帳には書かない（呼び出し側が決める。生成テストは書かない）。

## 2. `Adapter.Unavailable` の拡張

`Adapter.Unavailable(model)` は、これまで動画モデルだけを見ていた。画像モデルにも広げ、画像・動画のどちらのバックエンドにも無ければ `domain.ExcludedNoBackend`、ブレーカーが開いていれば `domain.ExcludedCircuitOpen` を返す（通常の選択・生成テストの一覧が同じ関数で判断する）。`Adapter.Snapshot()` は、いま使っている設定とバックエンド群の組を返す（呼び出しの途中で設定が置き換わっても、1 回の呼び出しは同じ設定を使う）。

## 3. シード

`ImageRequest.Seed *int64` と `VideoRequest.Seed *int64` を足した。nil はシードを指定しない（これまでの動作）。

| モデル | シード | 備考 |
|---|---|---|
| FLUX.2（BFL） | **送る**（`seed`） | |
| Runway Gen-4.5 | **送る**（`seed` は 0〜4294967295） | |
| ローカルの ComfyUI（ワークフローにシードの割り当てがある） | **送る** | `comfyui/graph.go` が割り当てへ入れる。割り当てが無ければ従来どおり乱数 |
| Nano Banana（Gemini）・Nano Banana Lite・GPT Image 2・Kling 3.0・Kling 4.0・Seedance 2.0・Seedance 2.5・Gemini Omni Flash・MiniMax H3 / H3 Max・Wan 3.0・ChatGPT サブスク ほか | 送らない | 同じ入力を本数ぶん呼ぶ。`seed` は NULL・`seed_sent=0`。Nano Banana は画像への効き方を実機で確認できるまで false（`modeladapter.SupportsSeed`）。Kling 4.0・Gemini Omni Flash・MiniMax は公式の本文に `seed` の欄が無い。Wan 3.0 は公式の本文に `seed` があるが、mam は送らない（`SupportsSeed` は偽） |

`modeladapter.SupportsSeed(model)` が組み込みの判断（FLUX.2・Runway だけ）、`direct.SupportsSeed(cfg, model)` がローカルのワークフローまで含めた判断。**シードはサーバーが作る**（`crypto/rand` の 32 ビット。Runway の範囲に収まる）。結果ごとに別の値を、**実際に送った値**で `gentest_result.seed` に保存する（再現のため）。利用者がシードを指定する欄はない。

## 4. ネガティブプロンプト

ネガティブの**欄を持つ**モデル（Kling 3.0、ネガティブの割り当てがあるワークフロー）はその欄へ送り、`negative_mode=field`。持たないモデルは、プロンプトの末尾に「Avoid: …」の 1 文として書き足して送り（`direct.Fold`）、`negative_mode=folded`、送った文を `sent_prompt` に残す。ネガティブを入れなければ `none`。

## 5. 上限の設定（`gentestcfg`）

`models.json` の `gentest`: `max_items`（既定 50、1 回のバッチの結果の数）・`max_cost_usd`（既定 20。0 は上限なし）。省略は既定。`configcheck` が値を検査し（負の数・0 以下の `max_items`・数値でない値を拒否）、設定画面の「画像・動画モデル」の保存・固定ファイルの規則（`MAM_MODELS_PINNED`）に従う。画面の見積りとジョブの投入の**両方**で検査する。

## 改訂履歴

[gentest.changelog.md](gentest.changelog.md)
