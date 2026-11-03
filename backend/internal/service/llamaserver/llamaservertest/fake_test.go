// Package llamaservertest tests the local LLM end to end over fakes: the llama.cpp runtime of the real catalog (its command line, its token, its models, its
// support rules and its texts) installed from a fake host, started as a process, chosen by the settings and called by the provider "local" (spec/architecture/
// model-manager/local-runtime.md §17, spec/architecture/local-llm.md §10). The server is the test binary itself, installed as "llama-server.exe": it
// reads the arguments the way llama-server does (and refuses one that llama-server has not, so the command line of the catalog stays one llama-server
// accepts), needs the key of the start (LLAMA_API_KEY) for every call but /health, and answers like the models do (a thinking switch that works for Gemma and
// Qwen, a DeepSeek that always thinks). Nothing of llama.cpp is run: a real installation and inference are device_only.
package llamaservertest_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// The name the test binary is installed under: when it is started as this, it is the fake llama-server.
const programName = "llama-server.exe"

// flags are the arguments of llama-server that the catalog uses; each takes one value but the ones in noValue.
var (
	withValue = []string{"-m", "--host", "--port", "--ctx-size", "-ngl", "--alias", "--parallel", "--sleep-idle-seconds"}
	noValue   = []string{"--no-webui", "--no-slots", "--offline"}
)

// maybeServeFakeLlama makes the test binary the fake llama-server when it was started under the name of the program; it reports whether it was.
func maybeServeFakeLlama() bool {
	if filepath.Base(os.Args[0]) != programName {
		return false
	}
	opts := map[string]string{}
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch {
		case slices.Contains(noValue, args[i]):
			opts[args[i]] = "true"
		case slices.Contains(withValue, args[i]) && i+1 < len(args):
			opts[args[i]] = args[i+1]
			i++
		default:
			fmt.Fprintf(os.Stderr, "error: invalid argument: %s\n", args[i])
			os.Exit(2)
		}
	}
	if opts["--host"] != "127.0.0.1" {
		fmt.Fprintln(os.Stderr, "error: the server must listen on 127.0.0.1")
		os.Exit(2)
	}
	if _, err := os.Stat(opts["-m"]); err != nil { // the model file is the one mam fetched, found from the environment directory
		fmt.Fprintf(os.Stderr, "error: failed to load model '%s'\n", opts["-m"])
		os.Exit(1)
	}
	key := os.Getenv("LLAMA_API_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "error: no API key: refusing to serve without one")
		os.Exit(3)
	}
	_ = os.WriteFile("argv.txt", []byte(strings.Join(os.Args[1:], "\n")), 0o600)
	l, err := net.Listen("tcp", "127.0.0.1:"+opts["--port"])
	if err != nil {
		os.Exit(4)
	}
	_ = (&http.Server{Handler: fakeHandler(opts["--alias"], key), ReadHeaderTimeout: time.Minute}).Serve(l)
	return true
}

// fakeHandler is the HTTP side of llama-server: /health is public, everything else needs "Authorization: Bearer <key>" (--api-key). The model answers to its
// --alias only.
func fakeHandler(alias, key string) http.Handler {
	var mu sync.Mutex
	var requests []map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"status":"ok"}`) })
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{{"id": alias, "object": "model"}}})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		requests = append(requests, body)
		mu.Unlock()
		if body["model"] != alias {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"message":"model not found"}}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": answer(alias, body)}}},
			"usage":   map[string]any{"prompt_tokens": 7, "completion_tokens": 9},
		})
	})
	// _requests is the fake's own door for the test: what the server was asked (llama-server has no such path).
	mux.HandleFunc("/_requests", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(requests)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" && r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"Invalid API Key","type":"authentication_error","code":401}}`)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// answer is what the model says. The chat template of Gemma and Qwen takes enable_thinking: off, it answers at once; on, it thinks first. DeepSeek R1's
// template puts the opening <think> in the prompt, so its answer carries the thinking and a closing tag only, and nothing turns that off.
func answer(alias string, body map[string]any) string {
	const json = `{"ok":true}`
	kwargs, _ := body["chat_template_kwargs"].(map[string]any)
	switch {
	case strings.HasPrefix(alias, "deepseek"):
		return "ユーザーは JSON が欲しい。{\"draft\": 1} ではなく ok を返そう。\n</think>\n\n" + json
	case kwargs["enable_thinking"] == false:
		return json
	}
	return "<think>文章を考える。{\"draft\": 1}</think>\n本文です。"
}
