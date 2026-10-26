package proc_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

// TestMain turns the test binary into the fake engine when MAM_FAKE_COMFY is set: the Manager starts
// os.Args[0] again, which then plays ComfyUI (the mode says how it behaves).
func TestMain(m *testing.M) {
	if mode := os.Getenv("MAM_FAKE_COMFY"); mode != "" {
		fakeComfy(mode)
		return
	}
	if os.Getenv("MAM_FAKE_STDIO") != "" {
		fakeStdio()
		return
	}
	if os.Getenv("MAM_FAKE_SUPERVISOR") != "" {
		supervisor()
		return
	}
	os.Exit(m.Run())
}

// fakeComfy listens on 127.0.0.1:$FAKE_PORT and answers /system_stats, like the real engine does once it is up.
func fakeComfy(mode string) {
	port := os.Getenv("FAKE_PORT")
	fmt.Println("fake comfyui starting, mode", mode)
	switch mode {
	case "exit-at-once":
		fmt.Fprintln(os.Stderr, "Traceback: ImportError: DLL load failed")
		os.Exit(3)
	case "never-ready":
		time.Sleep(time.Hour)
	}
	l, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(4)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/system_stats", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"system":{"comfyui_version":"fake"}}`))
	})
	if mode == "crash-later" {
		go func() { time.Sleep(300 * time.Millisecond); fmt.Fprintln(os.Stderr, "CUDA out of memory"); os.Exit(9) }()
	}
	_ = http.Serve(l, mux)
}

// build is the Config.Build of the tests: the test binary itself as the child.
func build(mode string, extraEnv ...string) func(int) (proc.Spec, error) {
	return func(port int) (proc.Spec, error) {
		return proc.Spec{
			Command: os.Args[0],
			Env:     append([]string{"MAM_FAKE_COMFY=" + mode, "FAKE_PORT=" + strconv.Itoa(port)}, extraEnv...),
		}, nil
	}
}

// supervisor plays mam: it starts the fake engine through a Manager, prints "<port> <pid>" and waits to be killed.
func supervisor() {
	m := proc.New(proc.Config{Build: build("serve"), PollEvery: 20 * time.Millisecond})
	st, err := m.Start(context.Background())
	if err != nil {
		fmt.Println("error", err)
		os.Exit(1)
	}
	fmt.Println(st.Port, st.PID)
	time.Sleep(time.Hour)
}
