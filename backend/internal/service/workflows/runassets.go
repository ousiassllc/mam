package workflows

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/audiofile"
)

func (s *Service) loadOne(ctx context.Context, ref *AssetRef, supported bool, what string) (*modeladapter.Input, error) {
	if ref == nil {
		return nil, nil
	}
	if !supported {
		return nil, invalid("このワークフローは%sを使いません", what)
	}
	in, err := s.loadAll(ctx, []AssetRef{*ref})
	if err != nil {
		return nil, err
	}
	return &in[0], nil
}

func (s *Service) loadAll(ctx context.Context, refs []AssetRef) ([]modeladapter.Input, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if s.LoadAsset == nil {
		return nil, invalid("画像の入力はこのアプリでは使えません")
	}
	out := make([]modeladapter.Input, 0, len(refs))
	for _, r := range refs {
		data, mime, err := s.LoadAsset(ctx, r.ProjectID, r.AssetID)
		if err != nil {
			return nil, err
		}
		if audiofile.Is(mime) { // an audio asset of the Project (Issue #1153) is no picture: the image inputs of a workflow take pictures only
			return nil, invalid("プロジェクトの音声ファイル %q は画像の入力に使えません", r.AssetID)
		}
		out = append(out, modeladapter.Input{Data: data, MimeType: mime})
	}
	return out, nil
}
