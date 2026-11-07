// Package ffmpegscreen_test is the tests of the section "ffmpeg" of the companion software of the model manager (spec/components/model-manager-screen.md, "付随ソフトの導入（#1106）")
// through the router: the paths of ffmpeg and ffprobe (Issue #856) and the download of ffmpeg (Issue #900), which answer at /model-manager/companion/ffmpeg… with the same
// bodies, statuses, roles and test ids that /settings/ffmpeg… had. The download is from a fake server: nothing real is fetched. It is a package of its own because internal/web is at its size limit.
package ffmpegscreen_test
