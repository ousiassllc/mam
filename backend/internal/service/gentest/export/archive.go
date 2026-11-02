package export

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const (
	// PhotosDir is the folder of the images and videos in the ZIP.
	PhotosDir = "photos/"
	// PromptsFile is the sheet, alone in the folder prompts/.
	PromptsFile = "prompts/prompts.csv"
	// ContentType is the media type of the ZIP.
	ContentType = "application/zip"
)

// bom makes Excel read the CSV as UTF-8.
const bom = "\uFEFF"

// header is the first row of prompts.csv.
var header = []string{"file", "created_at", "media", "model", "seed", "prompt", "sent_prompt", "result_id"}

// Archive is one ZIP to write.
type Archive struct {
	// Name is the file name the ZIP is offered under.
	Name  string
	Items []Item
}

// Write streams the ZIP to w: prompts/prompts.csv, then the files of photos/ one by one, each copied straight from the disk. The files
// are stored, not deflated: images and videos are compressed already. An error (a file that vanished, a closed connection) ends the
// stream before the central directory, so the half-written file is not a valid ZIP.
func (a Archive) Write(w io.Writer) error {
	zw := zip.NewWriter(w)
	if err := a.writeCSV(zw); err != nil {
		return err
	}
	for _, it := range a.Items {
		if err := copyFile(zw, it); err != nil {
			return err
		}
	}
	return zw.Close()
}

func (a Archive) writeCSV(zw *zip.Writer) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: PromptsFile, Method: zip.Deflate, Modified: newest(a.Items)})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, bom); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, it := range a.Items {
		if err := cw.Write(csvRow(it)); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// csvRow is the row of prompts.csv of an item. The cells that hold text somebody wrote (the model name, the prompts) cannot start a
// spreadsheet formula (see safeCell).
func csvRow(it Item) []string {
	seed := ""
	if it.row.SeedSent && it.row.Seed != nil {
		seed = strconv.FormatInt(*it.row.Seed, 10)
	}
	return []string{
		it.File, it.CreatedAt.In(jst).Format(csvLayout), it.Media, safeCell(it.Model), seed,
		safeCell(it.row.Prompt), safeCell(it.row.SentPrompt), it.ResultID,
	}
}

// safeCell keeps a spreadsheet from running a cell as a formula (CSV injection): a cell that starts with "=", "+", "-", "@", a tab
// or a carriage return gets a "'" in front, which Excel, LibreOffice and Google Sheets show as text. A prompt is free text (it may
// come from an AI client that read something hostile), and "-" or "@" at the start of a prompt is common, so the mark costs a
// character in the cell and nothing else; the original is in the history of mam.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func copyFile(zw *zip.Writer, it Item) error {
	src, err := os.Open(it.path) // #nosec G304 -- the path comes from the history's own record (Service.ResultFile), not from the request
	if err != nil {
		return fmt.Errorf("export: open %s: %w", it.ResultID, err)
	}
	defer func() { _ = src.Close() }()
	dst, err := zw.CreateHeader(&zip.FileHeader{Name: PhotosDir + it.File, Method: zip.Store, Modified: it.CreatedAt})
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("export: copy %s: %w", it.ResultID, err)
	}
	return nil
}
