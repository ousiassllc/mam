package fetch

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

var errRestart = errors.New("restart")

// accept decides what the answer is: a 206 that continues at have is appended; a 200 is the whole file again;
// a 416 means the part file is unusable.
func accept(res *http.Response, have int64, src Source) (resumed bool, err error) {
	switch res.StatusCode {
	case http.StatusPartialContent:
		start, ok := rangeStart(res.Header.Get("Content-Range"))
		if !ok || start != have {
			return false, fmt.Errorf("fetch: the server continued at the wrong offset (%q)", res.Header.Get("Content-Range"))
		}
		return true, nil
	case http.StatusOK:
		return false, nil
	case http.StatusRequestedRangeNotSatisfiable:
		if have > 0 {
			return false, errRestart
		}
	}
	return false, &HTTPError{Status: res.StatusCode, URL: redact(res.Request, src.URL)}
}

func redact(req *http.Request, fallback string) string {
	if req != nil && req.URL != nil {
		return req.URL.Scheme + "://" + req.URL.Host + req.URL.Path
	}
	return fallback
}

// rangeStart is the first byte of a Content-Range value "bytes 100-199/200".
func rangeStart(v string) (int64, bool) {
	rest, ok := strings.CutPrefix(v, "bytes ")
	if !ok {
		return 0, false
	}
	first, _, _ := strings.Cut(rest, "-")
	n, err := strconv.ParseInt(first, 10, 64)
	return n, err == nil
}
