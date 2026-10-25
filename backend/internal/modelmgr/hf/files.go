package hf

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Security statuses of the Hub's file scan (securityFileStatus.status).
const (
	SecuritySafe    = "safe"
	SecurityCaution = "caution"
	SecurityUnsafe  = "unsafe"
)

// FileInfo is what paths-info says about a file at a commit.
type FileInfo struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256,omitempty"`
	LFSSize int64  `json:"lfs_size,omitempty"`
	// Security is the status of the scan ("" when the Hub gave none).
	Security string `json:"security,omitempty"`
}

// pathsChunk is how many paths one paths-info request asks for.
const pathsChunk = 50

// PathsInfo reads the size, LFS SHA-256 and security scan of the files (expand=true). A path the commit does not have is
// missing from the result.
func (c *Client) PathsInfo(ctx context.Context, repo, commit string, paths []string) (map[string]FileInfo, error) {
	out := make(map[string]FileInfo, len(paths))
	for start := 0; start < len(paths); start += pathsChunk {
		chunk := paths[start:min(start+pathsChunk, len(paths))]
		var raw []struct {
			Type string `json:"type"`
			Path string `json:"path"`
			Size int64  `json:"size"`
			LFS  *struct {
				OID  string `json:"oid"`
				Size int64  `json:"size"`
			} `json:"lfs"`
			Security *struct {
				Status string `json:"status"`
			} `json:"securityFileStatus"`
		}
		body := map[string]any{"paths": chunk, "expand": true}
		if _, err := c.getJSON(ctx, request{method: http.MethodPost, path: apiPath(repo, "paths-info", commit), body: body}, &raw); err != nil {
			return nil, err
		}
		for _, e := range raw {
			if e.Type != "file" {
				continue
			}
			fi := FileInfo{Path: e.Path, Size: e.Size}
			if e.LFS != nil {
				fi.SHA256, fi.LFSSize = strings.ToLower(e.LFS.OID), e.LFS.Size
			}
			if e.Security != nil {
				fi.Security = strings.ToLower(e.Security.Status)
			}
			out[e.Path] = fi
		}
	}
	return out, nil
}

// MaxHeaderBytes bounds the JSON header of a .safetensors file that is read (real checkpoints have well under 8 MiB).
const MaxHeaderBytes = 16 << 20

// ErrHeader means the file does not start with a readable .safetensors header.
var ErrHeader = errors.New("hf: not a readable safetensors header")

// Header is the part of a .safetensors header the folder guess uses: the tensor names and the __metadata__ strings.
type Header struct {
	Names    []string          `json:"names"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ParseHeader reads the header of a .safetensors stream: an 8-byte little-endian length, then that many bytes of JSON.
func ParseHeader(r io.Reader) (Header, error) {
	var lenBuf [8]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return Header{}, fmt.Errorf("%w: %w", ErrHeader, err)
	}
	n := binary.LittleEndian.Uint64(lenBuf[:])
	if n == 0 || n > MaxHeaderBytes {
		return Header{}, fmt.Errorf("%w: header length %d", ErrHeader, n)
	}
	raw := make([]byte, n)
	if _, err := io.ReadFull(r, raw); err != nil {
		return Header{}, fmt.Errorf("%w: %w", ErrHeader, err)
	}
	return parseHeaderJSON(raw)
}

func parseHeaderJSON(raw []byte) (Header, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return Header{}, fmt.Errorf("%w: %w", ErrHeader, err)
	}
	h := Header{Names: make([]string, 0, len(top))}
	for name, v := range top {
		if name != "__metadata__" {
			h.Names = append(h.Names, name)
			continue
		}
		var m map[string]any
		if json.Unmarshal(v, &m) == nil {
			h.Metadata = make(map[string]string, len(m))
			for k, val := range m {
				if s, ok := val.(string); ok {
					h.Metadata[k] = s
				}
			}
		}
	}
	if len(h.Names) == 0 {
		return Header{}, fmt.Errorf("%w: no tensor", ErrHeader)
	}
	return h, nil
}

// ReadFileHeader reads the header of a .safetensors file on disk.
func ReadFileHeader(path string) (Header, error) {
	f, err := os.Open(path) // #nosec G304 -- a file the model manager downloaded
	if err != nil {
		return Header{}, err
	}
	defer func() { _ = f.Close() }()
	return ParseHeader(f)
}

// Header reads the header of a .safetensors file at the commit with two small Range requests (the 8-byte length, then
// the JSON), so no part of the weights is downloaded. The request follows the redirect to the CDN under the same rules
// as a download.
func (c *Client) Header(ctx context.Context, repo, commit, path string) (Header, error) {
	lenBytes, err := c.rangeGet(ctx, repo, commit, path, 0, 7)
	if err != nil {
		return Header{}, err
	}
	if len(lenBytes) != 8 {
		return Header{}, fmt.Errorf("%w: short read", ErrHeader)
	}
	n := binary.LittleEndian.Uint64(lenBytes)
	if n == 0 || n > MaxHeaderBytes {
		return Header{}, fmt.Errorf("%w: header length %d", ErrHeader, n)
	}
	raw, err := c.rangeGet(ctx, repo, commit, path, 8, 8+int64(n)-1) //nolint:gosec // n is at most MaxHeaderBytes
	if err != nil {
		return Header{}, err
	}
	if uint64(len(raw)) != n {
		return Header{}, fmt.Errorf("%w: short read", ErrHeader)
	}
	return parseHeaderJSON(raw)
}

// rangeGet reads bytes first..last of the file. A server that ignores Range (200) is cut after the wanted bytes.
func (c *Client) rangeGet(ctx context.Context, repo, commit, path string, first, last int64) ([]byte, error) {
	res, err := c.do(ctx, request{
		method: http.MethodGet, path: "/" + escapeSegments(repo) + "/resolve/" + commit + "/" + escapeSegments(path),
		header: http.Header{"Range": {"bytes=" + strconv.FormatInt(first, 10) + "-" + strconv.FormatInt(last, 10)}},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	switch res.StatusCode {
	case http.StatusPartialContent:
	case http.StatusOK:
		if first > 0 {
			if _, err := io.CopyN(io.Discard, res.Body, first); err != nil {
				return nil, err
			}
		}
	default:
		return nil, &APIError{Status: res.StatusCode, Code: res.Header.Get("X-Error-Code")}
	}
	return io.ReadAll(io.LimitReader(res.Body, last-first+1))
}

// Probe asks for the first byte of the file at the commit, to learn before a download whether the token may read it
// (a gated repository answers 401 or 403 until access is granted). A success is nil; anything else is an *APIError.
func (c *Client) Probe(ctx context.Context, repo, commit, path string) error {
	_, err := c.rangeGet(ctx, repo, commit, path, 0, 0)
	return err
}
