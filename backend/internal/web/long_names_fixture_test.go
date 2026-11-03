package web_test

import (
	"testing"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/jobs"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
)

// The wtest static/src/components/layout/long-names.wtest.ts draws these files with the real Tailwind CSS and checks that a long
// file name or model name stays inside its card (#1003: the progress line of an install, "Wan2.2_Remix_….safetensors（1/4）（0%）
// 107 MiB / 33.1 GiB", came out of the card). They are what the components render: `MAM_UPDATE_FIXTURES=1 go test
// ./backend/internal/web -run TestLongNamesFixtures` writes them again.
const (
	longFile  = "Wan2.2_Remix_NSFW_t2v_14b_high_lighting_v2.0.safetensors"
	longModel = "Wan2.2-Remix_NSFW_T2V_v2.0_fp8_scaled_high_noise_14B_lightning_distilled_checkpoint"
	longPath  = "split_files/diffusion_models/" + longFile
	longRepo  = "some-organisation-with-a-long-name/Wan2.2-Remix-NSFW-T2V-14B-High-Lighting-GGUF-and-fp8"
	longHash  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func longNamesProgress() *modelmgr.Progress {
	return &modelmgr.Progress{JobID: "job-1", Percent: 0, Message: longFile + "（1/4）", Bytes: "107 MiB / 33.1 GiB"}
}

func longNamesPage() templ.Component {
	catalog := scrollCatalog(1, []string{"job-1"})
	catalog.Models[0].Name = longModel
	catalog.Models[0].Install = longNamesProgress()
	hfFile := modelmgr.HFFile{Path: longPath, Name: longFile, Size: "33.1 GiB", SHA256: longHash, Format: "safetensors", Installable: true, Fit: "fits", Need: "24 GiB", Security: "safe"}
	return modelmgr.Body(modelmgr.Data{
		Engine:  modelmgr.Engine{Supported: true, Writable: true, Install: &modelmgr.Progress{JobID: "job-e", Percent: 12, Message: "ComfyUI_windows_portable_nvidia_cu128_" + longHash + ".7z", Bytes: "1.2 GiB / 2.1 GiB"}},
		Catalog: catalog,
		Trial:   scrollTrial(false),
		HFImport: modelmgr.HFImport{
			Kind: "text-to-image",
			Result: modelmgr.HFResult{
				Hits: []modelmgr.HFHit{{Repo: longRepo, Downloads: "1.2M", Likes: "3.4k", Gated: true, NFAA: true}},
				Repo: &modelmgr.HFRepo{ID: longRepo, Commit: longHash, Page: "https://huggingface.co/" + longRepo, Terms: "確認してください", Groups: []modelmgr.HFGroup{{Name: longFile, Files: []modelmgr.HFFile{hfFile}}}},
			},
			Models: modelmgr.HFModelList{
				Jobs:       []string{"job-2"},
				Installing: []modelmgr.HFJob{{Name: longFile, Progress: modelmgr.Progress{JobID: "job-2", Percent: 3, Message: longFile + "（1/4）", Bytes: "107 MiB / 33.1 GiB"}}},
				Models:     []modelmgr.HFModel{{Path: "diffusion_models/" + longFile, Name: longFile, Folder: "diffusion_models/" + longModel, Repo: longRepo, Commit: longHash, Size: "33.1 GiB"}},
			},
		},
	})
}

func longNamesJobs() templ.Component {
	return jobs.Section(jobs.List{Refresh: "/jobs/list", UpdatedAt: "10:00:00", Active: true, Running: 1, Rows: []jobs.Row{{
		ID: "job-1", Label: "導入", Subject: longModel, StateText: "実行中", Tone: atoms.BadgeInfo, StartedAt: "2026-11-03 10:00",
		Progress: &jobs.Progress{Percent: 0, Text: longFile + "（1/4） 107 MiB / 33.1 GiB"}, Error: longPath, Href: "/model-manager",
	}}})
}

func TestLongNamesFixtures_AreWhatTheComponentsRender(t *testing.T) {
	checkFixtures(t, map[string]templ.Component{
		"layout-long-names-model-manager.html": longNamesPage(),
		"layout-long-names-jobs.html":          longNamesJobs(),
	})
}
