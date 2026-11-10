package export_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/export"
)

var project = domain.Project{ID: "p1"}

func detect(t *testing.T, message string) map[commandinterpreter.OpType]export.Args {
	t.Helper()
	out := map[commandinterpreter.OpType]export.Args{}
	for _, op := range commandinterpreter.Detected(message) {
		switch op.Type {
		case export.OpExportVideo, export.OpDownloadExport, export.OpExportBundle:
			var a export.Args
			require.NoError(t, op.DecodeArgs(&a))
			out[op.Type] = a
		}
	}
	return out
}

func TestDetect_ReadsTheThreeRequestsWithoutAModel(t *testing.T) {
	for message, want := range map[string]commandinterpreter.OpType{
		"この動画を書き出して":             export.OpExportVideo,
		"これを書き出して":               export.OpExportVideo,
		"完成した動画をエクスポートしたい":       export.OpExportVideo,
		"さっきの書き出しをダウンロードしたい":     export.OpDownloadExport,
		"書き出した動画をダウンロードして":       export.OpDownloadExport,
		"動画をダウンロードしたい":           export.OpDownloadExport,
		"プロジェクトをバンドルで書き出したい":     export.OpExportBundle,
		"バックアップの ZIP が欲しい。書き出して": export.OpExportBundle,
		"プロジェクトをダウンロードしたい":       export.OpExportBundle,
		"Project をエクスポートして":      export.OpExportBundle,
	} {
		got := detect(t, message)
		require.Len(t, got, 1, message)
		assert.Contains(t, got, want, message)
	}
}

func TestDetect_ReadsTheChoicesOfTheExport(t *testing.T) {
	got := detect(t, "この動画を 9:16 の 720p、H.265 で書き出して")[export.OpExportVideo]
	assert.Equal(t, export.Args{AspectRatio: "9:16", Resolution: "720p", Codec: "h265"}, got)

	got = detect(t, "縦長の動画を4Kで書き出したい。webm で")[export.OpExportVideo]
	assert.Equal(t, export.Args{AspectRatio: "9:16", Resolution: "2160p", Codec: "vp9"}, got)

	got = detect(t, "この動画を書き出して")[export.OpExportVideo]
	assert.Equal(t, export.Args{}, got, "what is not named takes the Brief's default")
}

func TestDetect_DoesNotMistakeOtherMessagesForAnExport(t *testing.T) {
	for _, message := range []string{
		"書き出した動画は良かった",             // tells of an export
		"動画モデルをダウンロードして",           // the install operation's
		"Wan 2.2 の LoRA をダウンロードして", // the install operation's
		"ダウンロードしたい",                // nothing says what
		"カメラを固定して",                 // nothing about an export
		"この動画の雰囲気を変えて",             // a direction, not an export
		"OpenAI の API キーを設定したい",
		"動画モデルをエクスポートして", // not a video
		"予算を $30 に変えて",
		"Final QC を通して書き出せるようにして", // asks for no export of the video
	} {
		assert.NotContains(t, detect(t, message), export.OpExportVideo, message)
		assert.NotContains(t, detect(t, message), export.OpExportBundle, message)
		assert.NotContains(t, detect(t, message), export.OpDownloadExport, message)
	}
}

func TestAsked_TheModelsWordAloneIsNotEnough(t *testing.T) {
	for _, typ := range []commandinterpreter.OpType{export.OpExportVideo, export.OpDownloadExport, export.OpExportBundle} {
		spec, ok := commandinterpreter.SpecOf(typ)
		require.True(t, ok)
		assert.True(t, spec.AnyMode)
		assert.False(t, spec.Global, "an export belongs to the Project")
		assert.NotNil(t, spec.Asked)
		assert.NotEmpty(t, spec.Example)
		assert.NotEmpty(t, spec.Doc)
	}
	render, _ := commandinterpreter.NewOperation(export.OpExportVideo, export.Args{})
	dl := commandinterpreter.Operation{Type: export.OpDownloadExport}
	bundle := commandinterpreter.Operation{Type: export.OpExportBundle}
	ops := []commandinterpreter.Operation{render, dl, bundle}

	assert.Empty(t, commandinterpreter.Unasked(ops, "カメラを固定して"), "a message that asks for none drops all of them")
	assert.Empty(t, commandinterpreter.Unasked(ops, "動画の長さはこのままでいい"))
	kept := commandinterpreter.Unasked(ops, "この動画を書き出して")
	require.Len(t, kept, 1)
	assert.Equal(t, export.OpExportVideo, kept[0].Type)
}

func TestSpec_ConfirmsOnlyTheRenderAndAsksNothingOfTheLinks(t *testing.T) {
	render := commandinterpreter.Operation{Type: export.OpExportVideo}
	assert.True(t, render.Confirms())
	assert.False(t, render.Immediate())
	for _, typ := range []commandinterpreter.OpType{export.OpDownloadExport, export.OpExportBundle} {
		op := commandinterpreter.Operation{Type: typ}
		assert.True(t, op.Immediate(), typ)
		assert.False(t, op.Confirms(), typ)
	}
}

func TestClean_WritesTheChoicesTheWayTheServiceReadsThem(t *testing.T) {
	op, err := commandinterpreter.NewOperation(export.OpExportVideo, export.Args{AspectRatio: "9：16", Resolution: "4K", Codec: "HEVC"})
	require.NoError(t, err)

	cleaned, ok := commandinterpreter.Normalize(op)

	require.True(t, ok)
	var a export.Args
	require.NoError(t, cleaned.DecodeArgs(&a))
	assert.Equal(t, export.Args{AspectRatio: "9:16", Resolution: "2160p", Codec: "h265"}, a)

	dl, ok := commandinterpreter.Normalize(commandinterpreter.Operation{Type: export.OpDownloadExport, Args: []byte(`{"x":1}`)})
	require.True(t, ok)
	assert.Empty(t, dl.Args, "the links take no args")
}

type fakeEditor struct {
	view      export.View
	viewErr   error
	exported  []export.Args
	exportErr error
	done      export.Done
}

func (f *fakeEditor) deps() export.Deps {
	return export.Deps{
		View: func(context.Context, string) (export.View, error) { return f.view, f.viewErr },
		Export: func(_ context.Context, _ string, in export.Args) (export.Done, error) {
			if f.exportErr != nil {
				return export.Done{}, f.exportErr
			}
			f.exported = append(f.exported, in)
			return f.done, nil
		},
	}
}

func ready() export.View {
	return export.View{
		State: domain.StateReady, CanRender: true, HasAudio: true, AudioCurrent: true,
		Defaults: domain.ExportOptions{AspectRatio: "16:9", Resolution: "1080p", Codec: "h264"},
	}
}

func renderOp(t *testing.T, a export.Args) commandinterpreter.Operation {
	t.Helper()
	op, err := commandinterpreter.NewOperation(export.OpExportVideo, a)
	require.NoError(t, err)
	return op
}

func TestRender_TheCardShowsTheOptionsAgainstTheDefaultsAndChangesNothing(t *testing.T) {
	f := &fakeEditor{view: ready()}

	card, err := export.NewRender(f.deps()).Question(context.Background(), project, renderOp(t, export.Args{AspectRatio: "9:16"}))

	require.NoError(t, err)
	assert.Contains(t, card.Text, "9:16（既定: 16:9）")
	assert.Contains(t, card.Text, "1080p（既定）")
	assert.Contains(t, card.Text, "h264（既定）")
	assert.Contains(t, card.Text, "ffmpeg で書き出します（時間がかかります")
	assert.Contains(t, card.Text, "EXPORTED")
	assert.Empty(t, f.exported, "asking renders nothing")
	require.NotNil(t, card.Op, "the yes button carries the options the card shows")
	var shown export.Args
	require.NoError(t, card.Op.DecodeArgs(&shown))
	assert.Equal(t, export.Args{AspectRatio: "9:16", Resolution: "1080p", Codec: "h264"}, shown)
	body, err := chatops.Body(renderOp(t, export.Args{}), card)
	require.NoError(t, err)
	assert.Equal(t, []string{chatops.Yes, chatops.No}, body.Options)
}

func TestRender_AnAlreadyExportedProjectSaysItIsExportedAgain(t *testing.T) {
	v := ready()
	v.State = domain.StateExported
	f := &fakeEditor{view: v}

	card, err := export.NewRender(f.deps()).Question(context.Background(), project, renderOp(t, export.Args{}))

	require.NoError(t, err)
	assert.Contains(t, card.Text, "もう一度書き出します")
	assert.NotContains(t, card.Text, "EXPORTED になります")
}

func TestRender_NoCardWhenTheProjectCannotBeExported(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(*export.View)
		reason string
	}{
		"state":     {func(v *export.View) { v.State = domain.StateAudio }, "AUDIO"},
		"ffmpeg":    {func(v *export.View) { v.CanRender = false }, "ffmpeg"},
		"no audio":  {func(v *export.View) { v.HasAudio, v.AudioCurrent = false, false }, "音声がない"},
		"old audio": {func(v *export.View) { v.AudioCurrent = false }, "最新の編集"},
	} {
		v := ready()
		tc.change(&v)
		f := &fakeEditor{view: v}

		_, err := export.NewRender(f.deps()).Question(context.Background(), project, renderOp(t, export.Args{}))

		var ref *chatops.Refusal
		require.ErrorAs(t, err, &ref, name)
		assert.Contains(t, ref.Message, tc.reason, name)
		assert.Empty(t, f.exported, name)
	}
}

func TestRender_TheStateRefusalNamesWhatIsNeeded(t *testing.T) {
	v := ready()
	v.State = domain.StateFinalQC
	f := &fakeEditor{view: v}

	_, err := export.NewRender(f.deps()).Question(context.Background(), project, renderOp(t, export.Args{}))

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Contains(t, ref.Message, "FINAL_QC")
	assert.Contains(t, ref.Message, "Final QC に合格")
	assert.Contains(t, ref.Message, "READY")
}

func TestRender_AnOptionThatIsNoChoiceIsRefusedWithTheChoices(t *testing.T) {
	for _, a := range []export.Args{{AspectRatio: "3:2"}, {Resolution: "480p"}, {Codec: "prores"}} {
		f := &fakeEditor{view: ready()}

		_, err := export.NewRender(f.deps()).Question(context.Background(), project, renderOp(t, a))

		var ref *chatops.Refusal
		require.ErrorAs(t, err, &ref, a)
		assert.Contains(t, ref.Message, "から選んでください", a)
	}
}

func TestRender_ApplyExportsOnceWithTheOptionsOfTheCardAndGivesThePath(t *testing.T) {
	f := &fakeEditor{view: ready(), done: export.Done{
		ExportID: "e1", State: domain.StateExported, Options: domain.ExportOptions{AspectRatio: "9:16", Resolution: "720p", Codec: "vp9"},
	}}

	res, err := export.NewRender(f.deps()).Apply(context.Background(), project, renderOp(t, export.Args{AspectRatio: "9:16", Resolution: "720p", Codec: "vp9"}))

	require.NoError(t, err)
	assert.Equal(t, []export.Args{{AspectRatio: "9:16", Resolution: "720p", Codec: "vp9"}}, f.exported)
	assert.Contains(t, res.Text, "書き出しが完了しました（9:16 / 720p / vp9）")
	assert.Contains(t, res.Text, "EXPORTED")
	assert.Contains(t, res.Text, "/projects/p1/exports/e1/video")
	assert.Empty(t, res.Messages, "EditorService.Export posts its own notice with the download button: no second one")
}

func TestRender_ApplyPassesTheRefusalOfTheServiceOn(t *testing.T) {
	f := &fakeEditor{view: ready(), exportErr: chatops.Refuse("別の編集処理が実行中です")}

	_, err := export.NewRender(f.deps()).Apply(context.Background(), project, renderOp(t, export.Args{}))

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Empty(t, f.exported)

	f.exportErr = errors.New("disk full")
	_, err = export.NewRender(f.deps()).Apply(context.Background(), project, renderOp(t, export.Args{}))
	assert.EqualError(t, err, "disk full")
}

func TestDownload_AnswersWithTheLatestExportsLinkAndChangesNothing(t *testing.T) {
	v := ready()
	old := domain.Export{ID: "e1", AspectRatio: "16:9", Resolution: "1080p", Codec: "h264", ExportedAt: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)}
	newest := domain.Export{ID: "e2", AspectRatio: "9:16", Resolution: "720p", Codec: "h264", ExportedAt: time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)}
	v.Exports = []domain.Export{old, newest}
	f := &fakeEditor{view: v}

	res, err := export.NewDownload(f.deps()).Apply(context.Background(), project, commandinterpreter.Operation{Type: export.OpDownloadExport})

	require.NoError(t, err)
	assert.Contains(t, res.Text, "9:16 / 720p / h264")
	require.Len(t, res.Messages, 1)
	notice := res.Messages[0]
	assert.Equal(t, domain.RefExport, notice.RefType, "the chat draws the download button of an export notice")
	assert.Equal(t, "e2", notice.RefID)
	assert.Equal(t, domain.KindText, notice.Kind)
	assert.Empty(t, f.exported)
}

func TestDownload_WithoutAnExportSaysSoAndOffersToMakeOne(t *testing.T) {
	f := &fakeEditor{view: ready()}

	res, err := export.NewDownload(f.deps()).Apply(context.Background(), project, commandinterpreter.Operation{Type: export.OpDownloadExport})

	require.NoError(t, err)
	assert.Contains(t, res.Text, "まだ書き出した動画がありません")
	assert.Contains(t, res.Text, "書き出して")
	assert.Empty(t, res.Messages)
}

func TestDownload_NeverAsks(t *testing.T) {
	_, err := export.NewDownload(export.Deps{}).Question(context.Background(), project, commandinterpreter.Operation{})
	assert.ErrorIs(t, err, chatops.ErrNoChange)
	_, err = export.Bundle{}.Question(context.Background(), project, commandinterpreter.Operation{})
	assert.ErrorIs(t, err, chatops.ErrNoChange)
}

func TestBundle_AnswersWithTheRouteOfTheBundle(t *testing.T) {
	res, err := export.Bundle{}.Apply(context.Background(), domain.Project{ID: "p 1"}, commandinterpreter.Operation{Type: export.OpExportBundle})

	require.NoError(t, err)
	assert.Contains(t, res.Text, "/projects/p%201/bundle")
	assert.Contains(t, res.Text, "ZIP")
}

func TestPrompt_ListsTheThreeOperations(t *testing.T) {
	for _, typ := range []commandinterpreter.OpType{export.OpExportVideo, export.OpDownloadExport, export.OpExportBundle} {
		spec, _ := commandinterpreter.SpecOf(typ)
		assert.Contains(t, spec.Doc, `"`+string(typ)+`"`)
		assert.True(t, len(spec.Doc) > 0 && spec.Doc[len(spec.Doc)-1] == '\n')
	}
}
