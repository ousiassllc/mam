// Package config resolves runtime settings from flags and environment variables.
package config

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
)

// DefaultPort is the preferred listen port. The server falls back to a free port if it is taken.
const DefaultPort = 8787

// Config is the resolved runtime configuration.
type Config struct {
	// DataDir holds the SQLite database, logs and generated assets.
	DataDir string
	// Port is the preferred loopback port.
	Port int
	// OpenBrowser opens the default browser on startup.
	OpenBrowser bool
	// LogLevel is the minimum level written to the log file.
	LogLevel slog.Level
	// StaticDir, when set, serves static assets from disk instead of the embedded copy (make dev).
	StaticDir string
	// LLMConfigPath is a JSON file overriding the Reasoning LLM Adapter routes (agent.llm.LoadConfig);
	// empty selects the built-in assignment of spec/architecture/model-adapters.md §2.
	LLMConfigPath string
	// ModelConfigPath is a JSON file overriding the Model Adapter setting (agent.modeladapter.LoadConfig):
	// image/video model routes, prices, rate limits; empty selects spec/architecture/model-adapters.md §3, §4.
	ModelConfigPath string
	// JudgeConfigPath is a JSON file overriding the AI Video Judge setting (agent.judge.LoadConfig):
	// frame sampling and the Face Identity Similarity thresholds; empty selects the defaults.
	JudgeConfigPath string
	// FaceModelDir holds the face detection and embedding models (facesim.DetectorFile / RecognizerFile).
	// Empty selects FaceModels(): DataDir/models.
	FaceModelDir string
	// ONNXRuntimeLib is the ONNX Runtime shared library (1.23.x) the face models run on. Empty lets
	// the system search its standard library locations.
	ONNXRuntimeLib string
}

// DBPath is the SQLite database file.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "mam.db") }

// LogDir is the directory of rotated log files.
func (c Config) LogDir() string { return filepath.Join(c.DataDir, "logs") }

// AssetsDir is the root of generated and uploaded files.
func (c Config) AssetsDir() string { return filepath.Join(c.DataDir, "assets") }

// FaceModels is the directory of the face models: --face-model-dir, or DataDir/models.
func (c Config) FaceModels() string {
	if c.FaceModelDir != "" {
		return c.FaceModelDir
	}
	return filepath.Join(c.DataDir, "models")
}

// Load parses args (without the program name). Each flag defaults to its MAM_* environment variable.
func Load(args []string, getenv func(string) string, output io.Writer) (Config, error) {
	defaultDir, err := defaultDataDir()
	if err != nil && getenv("MAM_DATA_DIR") == "" {
		return Config{}, err
	}

	fs := flag.NewFlagSet("mam", flag.ContinueOnError)
	fs.SetOutput(output)
	dataDir := fs.String("data-dir", envOr(getenv, "MAM_DATA_DIR", defaultDir), "directory for the database, logs and assets (MAM_DATA_DIR)")
	port := fs.Int("port", envInt(getenv, "MAM_PORT", DefaultPort), "preferred loopback port; a free port is used if it is busy (MAM_PORT)")
	noBrowser := fs.Bool("no-browser", getenv("MAM_NO_BROWSER") != "", "do not open the default browser (MAM_NO_BROWSER)")
	level := fs.String("log-level", envOr(getenv, "MAM_LOG_LEVEL", "info"), "log level: debug, info, warn, error (MAM_LOG_LEVEL)")
	staticDir := fs.String("static-dir", getenv("MAM_STATIC_DIR"), "serve static assets from this directory instead of the embedded copy (MAM_STATIC_DIR)")
	llmConfig := fs.String("llm-config", getenv("MAM_LLM_CONFIG"), "JSON file with the Reasoning LLM provider assignment per agent (MAM_LLM_CONFIG)")
	modelConfig := fs.String("model-config", getenv("MAM_MODEL_CONFIG"), "JSON file with the image / video Model Adapter setting (MAM_MODEL_CONFIG)")
	judgeConfig := fs.String("judge-config", getenv("MAM_JUDGE_CONFIG"), "JSON file with the AI Video Judge setting: frame sampling and face similarity thresholds (MAM_JUDGE_CONFIG)")
	faceModels := fs.String("face-model-dir", getenv("MAM_FACE_MODEL_DIR"), "directory with the face detection and embedding models; default <data-dir>/models (MAM_FACE_MODEL_DIR)")
	onnxRuntime := fs.String("onnxruntime-lib", getenv("MAM_ONNXRUNTIME_LIB"), "ONNX Runtime shared library (1.23.x) for the face models (MAM_ONNXRUNTIME_LIB)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(*level)); err != nil {
		return Config{}, fmt.Errorf("invalid log level %q: %w", *level, err)
	}
	if *port < 0 || *port > 65535 {
		return Config{}, fmt.Errorf("invalid port %d", *port)
	}
	if *dataDir == "" {
		return Config{}, fmt.Errorf("data directory must not be empty")
	}
	return Config{
		DataDir:         *dataDir,
		Port:            *port,
		OpenBrowser:     !*noBrowser,
		LogLevel:        lvl,
		StaticDir:       *staticDir,
		LLMConfigPath:   *llmConfig,
		ModelConfigPath: *modelConfig,
		JudgeConfigPath: *judgeConfig,
		FaceModelDir:    *faceModels,
		ONNXRuntimeLib:  *onnxRuntime,
	}, nil
}

func defaultDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, "mam"), nil
}

func envOr(getenv func(string) string, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(getenv func(string) string, key string, fallback int) int {
	v := getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
