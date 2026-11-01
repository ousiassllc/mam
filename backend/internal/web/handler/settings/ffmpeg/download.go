package ffmpeg

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service/ffmpegsettings"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/ffmpeg"
)

// Services are what the download panel reads: the download itself and the lookup (to say whether ffmpeg is found already).
type Services struct {
	Settings *ffmpegsettings.Service
	Download *ffmpegdl.Service
}

// DownloadData is the panel of the download. Found: both tools are found by the lookup (with the saved paths and the downloaded
// copy); Managed: of the tools found, one is the downloaded copy.
func DownloadData(s Services) view.Download {
	st := s.Download.Status()
	d := view.Download{Supported: st.Supported}
	if !st.Supported {
		return d
	}
	d.Installed, d.Version, d.PinnedVersion, d.UpdateAvailable, d.Broken = st.Installed, st.Version, st.Pinned.Version, st.UpdateAvailable, st.Broken
	d.Dir, d.DownloadBytes, d.ExtractedBytes = st.Dir, st.DownloadBytes, st.ExtractedBytes
	d.Build, d.Publisher, d.SourceURL, d.SourceCodeURL = st.Pinned.Build, st.Pinned.Publisher, st.Pinned.SourceURL, st.Pinned.SourceCodeURL
	d.License, d.LicenseURL = st.Pinned.License.Name, st.Pinned.License.URL
	d.Install = view.DownloadInstall{
		Running: st.Install.State == ffmpegdl.InstallRunning, Percent: st.Install.Percent, Message: st.Install.Message,
		Error: st.Install.Error, Cancelled: st.Install.Cancelled, Succeeded: st.Install.State == ffmpegdl.InstallSucceeded,
	}
	if s.Settings != nil {
		if v, err := s.Settings.View(); err == nil {
			d.Found = v.FFmpeg.Path != "" && v.FFprobe.Path != ""
			d.Managed = inDir(v.FFmpeg.Path, st.Dir) || inDir(v.FFprobe.Path, st.Dir)
		}
	}
	return d
}

// inDir is whether the file at path lies directly in dir.
func inDir(path, dir string) bool {
	return path != "" && dir != "" && strings.EqualFold(filepath.Clean(filepath.Dir(path)), filepath.Clean(dir))
}

// panel answers with the panel: with the result of the action in hand.
func panel(c *gin.Context, s Services, status int, notice, errText string) {
	d := DownloadData(s)
	d.Notice, d.Error = notice, errText
	c.Header("Cache-Control", "no-store")
	settingshttp.Render(c, status, view.DownloadPanel(d))
}

// DownloadPanel answers with the panel (GET /settings/ffmpeg/download): what the panel polls while a download runs. A request
// that is not htmx gets the page of the section.
func DownloadPanel(s Services, page gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settingshttp.IsHTMX(c) {
			page(c)
			return
		}
		panel(c, s, http.StatusOK, "", "")
	}
}

// StartDownload starts the download of the pinned ffmpeg (POST /settings/ffmpeg/download). It answers at once: the panel polls
// the progress.
func StartDownload(s Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		started, err := s.Download.StartInstall()
		var noSpace *fetch.NoSpaceError
		switch {
		case err == nil && started:
			panel(c, s, http.StatusOK, "ダウンロードを始めました", "")
		case err == nil:
			panel(c, s, http.StatusOK, "すでにダウンロード中です", "")
		case errors.Is(err, ffmpegdl.ErrBusy):
			panel(c, s, http.StatusConflict, "", "ダウンロードした ffmpeg を削除しています。終わってからもう一度お試しください")
		case errors.Is(err, ffmpegdl.ErrUnsupported):
			panel(c, s, http.StatusConflict, "", "この環境ではダウンロードできません（できるのは Windows（x64）と macOS（Apple Silicon）だけです）。自分で入れた ffmpeg のパスを指定してください")
		case errors.Is(err, exeinstall.ErrNotWritable):
			panel(c, s, http.StatusConflict, "", fmt.Sprintf("ffmpeg の保存先（%s）に書き込めません。書き込み権限を確認してください（Windows 版は mam の実行ファイルの置き場所、macOS 版はデータフォルダの中です）", s.Download.Status().Dir))
		case errors.As(err, &noSpace):
			panel(c, s, http.StatusInsufficientStorage, "", fmt.Sprintf("ディスクの空きが足りません（必要 約 %d MB、空き %d MB）", noSpace.Need>>20, noSpace.Free>>20))
		default:
			_ = c.Error(err)
			panel(c, s, http.StatusInternalServerError, "", "ダウンロードを始められませんでした。mam のログを確認してください")
		}
	}
}

// CancelDownload stops the download (POST /settings/ffmpeg/download/cancel); downloading again resumes it.
func CancelDownload(s Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		s.Download.CancelInstall()
		s.Download.WaitInstall()
		panel(c, s, http.StatusOK, "", "")
	}
}

// RemoveDownload deletes the downloaded copy (DELETE /settings/ffmpeg/download). mam keeps using the copy it started with until
// it is started again.
func RemoveDownload(s Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := s.Download.Remove()
		switch {
		case err == nil:
			panel(c, s, http.StatusOK, "削除しました。起動中の mam は、次に起動し直すまで、これまでの ffmpeg を使い続けます", "")
		case errors.Is(err, ffmpegdl.ErrBusy):
			panel(c, s, http.StatusConflict, "", "ダウンロード中は削除できません。中断してからもう一度お試しください")
		case errors.Is(err, ffmpegdl.ErrUnsupported):
			panel(c, s, http.StatusConflict, "", "この環境ではダウンロードした ffmpeg はありません")
		default:
			_ = c.Error(err)
			panel(c, s, http.StatusInternalServerError, "", "削除できませんでした。ffmpeg が使われていないか確認してください（mam のログに理由があります）")
		}
	}
}
