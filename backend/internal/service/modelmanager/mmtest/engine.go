package mmtest

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

// fakeEngine starts this test binary as the engine: Main (called by TestMain) listens on the port and answers /system_stats.
func fakeEngine(port int) (proc.Spec, error) {
	return proc.Spec{Command: os.Args[0], Env: []string{"MAM_FAKE_COMFY=1", "FAKE_PORT=" + strconv.Itoa(port)}}, nil
}

// Main is for the TestMain of a package that starts the fake engine: when the binary was started as the engine it
// serves /system_stats and never returns; otherwise it runs the tests.
//
//	func TestMain(m *testing.M) { mmtest.Main(m) }
func Main(m *testing.M) {
	if os.Getenv("MAM_FAKE_COMFY") != "" {
		l, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("FAKE_PORT"))
		if err != nil {
			os.Exit(4)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/system_stats", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"system":{"comfyui_version":"fake"}}`))
		})
		_ = (&http.Server{Handler: mux, ReadHeaderTimeout: time.Minute}).Serve(l)
		return
	}
	os.Exit(m.Run())
}
