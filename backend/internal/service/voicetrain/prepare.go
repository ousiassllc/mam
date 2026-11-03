package voicetrain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

// job is the job.json the driver reads: every path is inside Work.
type job struct {
	Work       string `json:"work_dir"`
	List       string `json:"list_path"`
	Wav        string `json:"wav_dir"`
	Exp        string `json:"exp_dir"`
	Out        string `json:"out_dir"`
	SoVITSDir  string `json:"sovits_weights_dir"`
	GPTDir     string `json:"gpt_weights_dir"`
	ExpName    string `json:"exp_name"`
	GPU        string `json:"gpu"`
	EpochsS2   int    `json:"epochs_sovits"`
	EpochsS1   int    `json:"epochs_gpt"`
	BatchS2    int    `json:"batch_sovits"`
	BatchS1    int    `json:"batch_gpt"`
	Clips      int    `json:"clips"`
	configPath string
}

// batchFor is the batch size the vendor's webui proposes for a GPU: half of its video memory in GB (webui.py set_default), at least 1.
func batchFor(vramGB int) int { return min(max(vramGB/2, 1), 32) }

func clipBytes(clips []Clip) int64 {
	var n int64
	for _, c := range clips {
		n += c.SizeBytes
	}
	return n
}

// checkDisk is the common error disk_full when the volume of path has less than need free.
func (s *Service) checkDisk(path string, need int64) error {
	if path == "" {
		return nil
	}
	free, err := s.FreeBytes(path)
	if err != nil {
		return fmt.Errorf("free space of %s: %w", path, err)
	}
	if free < uint64(need) { //nolint:gosec // need is a sum of sizes
		return apperr.New(apperr.CodeDiskFull).With("where", fmt.Sprintf("（学習に約 %s 必要で、空きは %s です。場所: %s）", humanBytes(uint64(need)), humanBytes(free), path)) //nolint:gosec // same
	}
	return nil
}

func humanBytes(n uint64) string {
	const gib = 1 << 30
	if n >= gib {
		return fmt.Sprintf("%.1f GiB", float64(n)/gib)
	}
	return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
}

// prepare makes the working directory of the training: the clips (copied, so that the draft can change meanwhile), the list file of the vendor's
// scripts ("<file>|<speaker>|<language>|<text>") and the job file. A failure removes what was made.
func (s *Service) prepare(o voicerefsvc.Owner, characterID, id, base string, clips []Clip, lang string, opt Options, vramGB int) (j job, err error) {
	abs, err := filepath.Abs(filepath.Join(base, id))
	if err != nil {
		return job{}, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(abs)
		}
	}()
	j = job{
		Work: abs, List: filepath.Join(abs, "data", "data.list"), Wav: filepath.Join(abs, "data"), Exp: filepath.Join(abs, "exp"), Out: filepath.Join(abs, "out"),
		SoVITSDir: filepath.Join(abs, "weights_sovits"), GPTDir: filepath.Join(abs, "weights_gpt"), ExpName: "mam-" + id, GPU: "0",
		EpochsS2: opt.EpochsSoVITS, EpochsS1: opt.EpochsGPT, BatchS2: batchFor(vramGB), BatchS1: batchFor(vramGB), Clips: len(clips),
		configPath: filepath.Join(abs, "job.json"),
	}
	if err := os.MkdirAll(j.Wav, 0o700); err != nil {
		return job{}, fmt.Errorf("create the working directory: %w", err)
	}
	src := s.dir(o, characterID)
	var list strings.Builder
	for _, c := range clips {
		if err := fsutil.CopyFile(filepath.Join(src, c.File), filepath.Join(j.Wav, c.File)); err != nil {
			return job{}, fmt.Errorf("copy the clip %s: %w", c.File, err)
		}
		fmt.Fprintf(&list, "%s|mam|%s|%s\n", c.File, lang, c.Text)
	}
	if err := os.WriteFile(j.List, []byte(list.String()), 0o600); err != nil {
		return job{}, fmt.Errorf("write the list of the clips: %w", err)
	}
	raw, err := json.Marshal(j)
	if err != nil {
		return job{}, err
	}
	if err := os.WriteFile(j.configPath, raw, 0o600); err != nil {
		return job{}, fmt.Errorf("write the job: %w", err)
	}
	return j, nil
}
