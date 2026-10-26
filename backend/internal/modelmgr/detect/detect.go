// Package detect finds a ComfyUI that is already on the machine (spec/architecture/model-manager/model-manager.md §3.6): one
// that answers GET /system_stats on its usual port, and the configuration file of ComfyUI Desktop. A ComfyUI that
// is not running cannot be found over HTTP; the user then types its URL. Anything found is used as an external
// endpoint: mam never starts or stops it.
package detect

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Ports a ComfyUI listens on by default: the portable build and the command line use 8188, ComfyUI Desktop 8000
// (DEFAULT_SERVER_ARGS.port of Comfy-Org/desktop).
var Ports = []int{8188, DesktopPort}

// DesktopPort is the port of ComfyUI Desktop.
const DesktopPort = 8000

// maxBody bounds what a probe reads: /system_stats is a few KB.
const maxBody = 1 << 20

// Found is a ComfyUI that answered.
type Found struct {
	URL     string `json:"url"`
	Port    int    `json:"port"`
	Version string `json:"version"`
	// Desktop: the answer came from the port ComfyUI Desktop uses (an Electron app mam cannot control).
	Desktop bool     `json:"desktop"`
	Devices []Device `json:"devices,omitempty"`
}

// Device is a GPU as /system_stats lists it.
type Device struct {
	Name      string `json:"name"`
	VRAMTotal int64  `json:"vram_total"`
	VRAMFree  int64  `json:"vram_free"`
}

// DesktopConfig is the hint of an installed ComfyUI Desktop: %APPDATA%\ComfyUI\config.json exists (it does not
// say the app is running). BasePath is the folder with its models, when the file names one.
type DesktopConfig struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	BasePath string `json:"base_path,omitempty"`
}

// Report is what --detect-comfyui prints.
type Report struct {
	Found   []Found       `json:"found"`
	Desktop DesktopConfig `json:"desktop_config"`
	// Recommended is the URL to use as the external endpoint: the first ComfyUI that answered ("" for none).
	Recommended string `json:"recommended_url"`
}

// Options of Detect. The zero value probes 127.0.0.1 on Ports with a 1.5 second timeout per request.
type Options struct {
	// Client sends the probes (nil: a client with the Timeout, no proxy).
	Client *http.Client
	// Host is the host probed (default 127.0.0.1).
	Host string
	// Ports replaces Ports; DesktopPort (default the constant) is the one that marks ComfyUI Desktop.
	Ports       []int
	DesktopPort int
	// ConfigDir is the directory of the per-user configuration (os.UserConfigDir, %APPDATA% on Windows).
	ConfigDir string
	Timeout   time.Duration
}

// Detect probes the ports and reads the Desktop configuration.
func Detect(ctx context.Context, o Options) Report {
	if o.Host == "" {
		o.Host = "127.0.0.1"
	}
	if o.Ports == nil {
		o.Ports = Ports
	}
	if o.DesktopPort == 0 {
		o.DesktopPort = DesktopPort
	}
	if o.Timeout <= 0 {
		o.Timeout = 1500 * time.Millisecond
	}
	if o.Client == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.Proxy = nil
		o.Client = &http.Client{Transport: tr, Timeout: o.Timeout}
	}
	r := Report{Found: []Found{}, Desktop: readDesktopConfig(o.ConfigDir)}
	for _, port := range o.Ports {
		if f, ok := probe(ctx, o, port); ok {
			r.Found = append(r.Found, f)
		}
	}
	if len(r.Found) > 0 {
		r.Recommended = r.Found[0].URL
	}
	return r
}

// Probe asks base (http://host:port) for /system_stats; ok is true for a ComfyUI that answered with a version.
func probe(ctx context.Context, o Options, port int) (Found, bool) {
	base := "http://" + o.Host + ":" + strconv.Itoa(port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/system_stats", nil)
	if err != nil {
		return Found{}, false
	}
	res, err := o.Client.Do(req)
	if err != nil {
		return Found{}, false
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return Found{}, false
	}
	var body struct {
		System struct {
			Version string `json:"comfyui_version"`
		} `json:"system"`
		Devices []struct {
			Name      string `json:"name"`
			VRAMTotal int64  `json:"vram_total"`
			VRAMFree  int64  `json:"vram_free"`
		} `json:"devices"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxBody)).Decode(&body); err != nil || body.System.Version == "" {
		return Found{}, false
	}
	f := Found{URL: base, Port: port, Version: body.System.Version, Desktop: port == o.DesktopPort}
	for _, d := range body.Devices {
		f.Devices = append(f.Devices, Device{Name: d.Name, VRAMTotal: d.VRAMTotal, VRAMFree: d.VRAMFree})
	}
	return f, true
}

// readDesktopConfig reads <configDir>/ComfyUI/config.json. A file that is not valid JSON still counts as present.
func readDesktopConfig(configDir string) DesktopConfig {
	if configDir == "" {
		return DesktopConfig{}
	}
	c := DesktopConfig{Path: filepath.Join(configDir, "ComfyUI", "config.json")}
	data, err := os.ReadFile(c.Path)
	if errors.Is(err, os.ErrNotExist) || err != nil {
		return c
	}
	c.Exists = true
	var cfg struct {
		BasePath string `json:"basePath"`
	}
	if json.Unmarshal(data, &cfg) == nil {
		c.BasePath = cfg.BasePath
	}
	return c
}
