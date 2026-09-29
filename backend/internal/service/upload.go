package service

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// Upload limits (spec/requirements/non-functional.md §5 入力検証).
const (
	MaxAttachments   = 8
	MaxImageBytes    = 20 << 20
	MaxVideoBytes    = 300 << 20
	sniffBytes       = 512
	assetFilePerm    = 0o600
	assetDirPerm     = 0o700
	ftypOffset       = 4
	ftypBrandOffset  = 8
	ftypBrandEnd     = 12
	quickTimeBrand   = "qt  "
	mimeQuickTime    = "video/quicktime"
	mimeMP4          = "video/mp4"
	mimeWebM         = "video/webm"
	unsupportedTypes = "対応していないファイル形式です（画像: jpg/png/webp/gif、動画: mp4/mov/webm）"
)

// Upload is one file the user attached. Open is called at most once.
type Upload struct {
	Name string
	Size int64
	Open func() (io.ReadCloser, error)
}

type fileKind struct {
	assetType domain.AssetType
	mime      string
	maxBytes  int64
}

var extensions = map[string]fileKind{
	".jpg":  {domain.AssetImage, "image/jpeg", MaxImageBytes},
	".jpeg": {domain.AssetImage, "image/jpeg", MaxImageBytes},
	".png":  {domain.AssetImage, "image/png", MaxImageBytes},
	".webp": {domain.AssetImage, "image/webp", MaxImageBytes},
	".gif":  {domain.AssetImage, "image/gif", MaxImageBytes},
	".mp4":  {domain.AssetVideo, mimeMP4, MaxVideoBytes},
	".mov":  {domain.AssetVideo, mimeQuickTime, MaxVideoBytes},
	".webm": {domain.AssetVideo, mimeWebM, MaxVideoBytes},
}

// storedUpload is an Upload that passed validation and was written to disk.
type storedUpload struct {
	asset domain.Asset
	path  string
}

// storeUploads validates every upload (size, extension, content type) and writes the
// files below the assets directory as assets of projectID. Nothing stays on disk
// when any upload is rejected. The caller owns the returned files: it removes them
// with discardUploads when persisting the rows fails.
func (s *core) storeUploads(projectID string, uploads []Upload) ([]storedUpload, error) {
	if len(uploads) == 0 {
		return nil, nil
	}
	if len(uploads) > MaxAttachments {
		return nil, invalid("attachments", "添付は一度に %d 件までです", MaxAttachments)
	}
	if s.assetsDir == "" {
		return nil, errors.New("uploads are not configured: assets directory is empty")
	}
	var stored []storedUpload
	for _, u := range uploads {
		su, err := s.storeUpload(projectID, u)
		if err != nil {
			discardUploads(stored)
			return nil, err
		}
		stored = append(stored, su)
	}
	return stored, nil
}

func (s *core) storeUpload(projectID string, u Upload) (storedUpload, error) {
	name := filepath.Base(u.Name)
	ext := strings.ToLower(filepath.Ext(name))
	kind, ok := extensions[ext]
	if !ok {
		return storedUpload{}, invalid("attachments", "%s: %s", name, unsupportedTypes)
	}
	if u.Size <= 0 {
		return storedUpload{}, invalid("attachments", "%s: ファイルが空です", name)
	}
	if u.Size > kind.maxBytes {
		return storedUpload{}, invalid("attachments", "%s: サイズが上限（%d MB）を超えています", name, kind.maxBytes>>20)
	}
	rc, err := u.Open()
	if err != nil {
		return storedUpload{}, fmt.Errorf("open upload %q: %w", name, err)
	}
	defer func() { _ = rc.Close() }()

	head := make([]byte, sniffBytes)
	n, err := io.ReadFull(rc, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return storedUpload{}, fmt.Errorf("read upload %q: %w", name, err)
	}
	head = head[:n]
	if !contentMatches(ext, kind, head) {
		return storedUpload{}, invalid("attachments", "%s: 拡張子とファイルの内容が一致しません", name)
	}

	id := domain.NewID()
	rel := filepath.Join(projectID, id+ext)
	abs := filepath.Join(s.assetsDir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), assetDirPerm); err != nil {
		return storedUpload{}, fmt.Errorf("create asset directory: %w", err)
	}
	var written int64
	err = fsutil.WriteAtomic(abs, assetFilePerm, func(w io.Writer) error {
		var copyErr error
		// One byte over the limit is enough to notice a lying Size.
		written, copyErr = io.Copy(w, io.LimitReader(io.MultiReader(bytes.NewReader(head), rc), kind.maxBytes+1))
		return copyErr
	})
	if err != nil {
		return storedUpload{}, fmt.Errorf("store upload %q: %w", name, err)
	}
	if written > kind.maxBytes {
		_ = os.Remove(abs)
		return storedUpload{}, invalid("attachments", "%s: サイズが上限（%d MB）を超えています", name, kind.maxBytes>>20)
	}
	return storedUpload{
		asset: domain.Asset{
			ID: id, ProjectID: projectID, Type: kind.assetType,
			FilePath: filepath.ToSlash(rel), MimeType: kind.mime, SizeBytes: written,
		},
		path: abs,
	}, nil
}

func discardUploads(stored []storedUpload) {
	for _, su := range stored {
		_ = os.Remove(su.path)
	}
}

func assetsOf(stored []storedUpload) []domain.Asset {
	assets := make([]domain.Asset, len(stored))
	for i, su := range stored {
		assets[i] = su.asset
	}
	return assets
}

// contentMatches checks the file signature against the extension, so a renamed
// executable or document is rejected even though its name looks harmless.
func contentMatches(ext string, kind fileKind, head []byte) bool {
	if kind.assetType == domain.AssetImage {
		detected := http.DetectContentType(head)
		return detected == kind.mime
	}
	switch ext {
	case ".webm":
		return bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}) // EBML header
	case ".mp4", ".mov":
		if len(head) < ftypBrandEnd || string(head[ftypOffset:ftypBrandOffset]) != "ftyp" {
			return false
		}
		quickTime := string(head[ftypBrandOffset:ftypBrandEnd]) == quickTimeBrand
		return ext == ".mov" || !quickTime
	}
	return false
}
