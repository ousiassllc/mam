package localmodels

import (
	"strconv"
	"strings"
)

func itoa(n int) string { return strconv.Itoa(n) }

func joinNames(names []string) string { return strings.Join(names, "、") }

func keyRowID(endpoint string) string { return "local-key-row-" + endpoint }

// keyState is "設定済み（末尾 abcd）": the last characters when the key is long enough to show them.
func keyState(k Key) string {
	if k.LastChars == "" {
		return "設定済み"
	}
	return "設定済み（末尾 " + k.LastChars + "）"
}

func keyPlaceholder(configured bool) string {
	if configured {
		return "置き換えるキーを入力"
	}
	return "キーを入力（不要なら空のまま）"
}

// kindLabel is the name of an endpoint kind on the screen.
func kindLabel(kind string) string {
	if kind == KindComfyUI {
		return "ComfyUI"
	}
	return "OpenAI 互換"
}

func mediaLabel(media string) string {
	if media == MediaVideo {
		return "動画"
	}
	return "画像"
}

// slotLabels names the values of mam a workflow takes, in the order of the table.
var slotLabels = map[string]string{
	"prompt": "Prompt", "negative_prompt": "Negative Prompt", "seed": "Seed", "width": "幅", "height": "高さ",
	"frames": "フレーム数", "fps": "fps（入力）", "reference_image": "参照画像", "first_frame": "最初のフレーム", "last_frame": "最後のフレーム",
}

// SlotLabel is the label of the table row of a workflow parameter; index is the reference image number.
func SlotLabel(param string, index int) string {
	label := slotLabels[param]
	if param == "reference_image" {
		return label + " " + itoa(index+1)
	}
	return label
}
