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
	"strings"
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
	// ModelConfigPath is a JSON file overriding the Model Adapter setting (agent/modeladapter/configcheck.Load):
	// image/video model routes, prices, rate limits; empty selects spec/architecture/model-adapters.md §3, §4.
	ModelConfigPath string
	// JudgeConfigPath is a JSON file overriding the AI Video Judge setting (agent.judge.LoadConfig):
	// frame sampling and the Face Identity Similarity thresholds; empty selects the defaults.
	JudgeConfigPath string
	// JevConfigPath is a JSON file overriding the Jev shadow-run setting (agent.jev.LoadConfig): the
	// pinned version, base URL, timeout and price; empty selects the defaults.
	JevConfigPath string
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

// LLMSettingsFile is the file of the Reasoning LLM setting (the provider and model of each Agent) and
// whether the operator pinned it. --llm-config / MAM_LLM_CONFIG (LLMConfigPath) is pinned: the screen
// must not write it. Otherwise it is DataDir/llm.json, which the server loads when it exists and the
// settings screen may write.
func (c Config) LLMSettingsFile() (path string, pinned bool) {
	return settingsFile(c.LLMConfigPath, filepath.Join(c.DataDir, "llm.json"))
}

// ModelSettingsFile is the file of the image / video Model Adapter setting and whether the operator
// pinned it: --model-config / MAM_MODEL_CONFIG (ModelConfigPath), else DataDir/models.json (see LLMSettingsFile).
func (c Config) ModelSettingsFile() (path string, pinned bool) {
	return settingsFile(c.ModelConfigPath, filepath.Join(c.DataDir, "models.json"))
}

func settingsFile(flagPath, dataDirPath string) (path string, pinned bool) {
	if flagPath != "" {
		return flagPath, true
	}
	return dataDirPath, false
}

// FaceModels is the directory of the face models: --face-model-dir, or DataDir/models.
func (c Config) FaceModels() string {
	if c.FaceModelDir != "" {
		return c.FaceModelDir
	}
	return filepath.Join(c.DataDir, "models")
}

// Load parses args (without the program name). Each flag defaults to its MAM_* environment variable.
func Load(args []string, getenv func(string) string, output io.Writer) (Config, error) {
	// An unresolvable default is an error only when neither --data-dir nor MAM_DATA_DIR replaces it
	// (checked after the flags are parsed, so --help works everywhere).
	defaultDir, defaultErr := defaultDataDir()

	fs := flag.NewFlagSet("mam", flag.ContinueOnError)
	fs.SetOutput(output)
	dataDir := fs.String("data-dir", envOr(getenv, "MAM_DATA_DIR", defaultDir), "directory for the database, logs and assets (MAM_DATA_DIR)")
	port := fs.Int("port", DefaultPort, "preferred loopback port; a free port is used if it is busy (MAM_PORT)")
	noBrowser := fs.Bool("no-browser", false, "do not open the default browser (MAM_NO_BROWSER)")
	level := fs.String("log-level", envOr(getenv, "MAM_LOG_LEVEL", "info"), "log level: debug, info, warn, error (MAM_LOG_LEVEL)")
	staticDir := fs.String("static-dir", getenv("MAM_STATIC_DIR"), "serve static assets from this directory instead of the embedded copy (MAM_STATIC_DIR)")
	llmConfig := fs.String("llm-config", getenv("MAM_LLM_CONFIG"), "JSON file with the Reasoning LLM provider assignment per agent (MAM_LLM_CONFIG)")
	modelConfig := fs.String("model-config", getenv("MAM_MODEL_CONFIG"), "JSON file with the image / video Model Adapter setting (MAM_MODEL_CONFIG)")
	judgeConfig := fs.String("judge-config", getenv("MAM_JUDGE_CONFIG"), "JSON file with the AI Video Judge setting: frame sampling and face similarity thresholds (MAM_JUDGE_CONFIG)")
	jevConfig := fs.String("jev-config", getenv("MAM_JEV_CONFIG"), "JSON file with the TypeSafe Jev shadow-run setting of the Repair Agent: pinned version, base URL, timeout, price (MAM_JEV_CONFIG)")
	faceModels := fs.String("face-model-dir", getenv("MAM_FACE_MODEL_DIR"), "directory with the face detection and embedding models; default <data-dir>/models (MAM_FACE_MODEL_DIR)")
	onnxRuntime := fs.String("onnxruntime-lib", getenv("MAM_ONNXRUNTIME_LIB"), "ONNX Runtime shared library (1.23.x) for the face models (MAM_ONNXRUNTIME_LIB)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	// MAM_PORT is read only when --port is absent, and only after the parse, so a malformed value
	// breaks neither --help nor a command line that overrides it.
	if !flagSet(fs, "port") {
		envPort, err := envInt(getenv, "MAM_PORT", DefaultPort)
		if err != nil {
			return Config{}, err
		}
		*port = envPort
	}

	// MAM_NO_BROWSER is read only when --no-browser is absent, for the same reason as MAM_PORT.
	if !flagSet(fs, "no-browser") {
		envNoBrowser, err := envBool(getenv, "MAM_NO_BROWSER")
		if err != nil {
			return Config{}, err
		}
		*noBrowser = envNoBrowser
	}

	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(*level)); err != nil {
		return Config{}, fmt.Errorf("invalid log level %q: %w", *level, err)
	}
	if *port < 0 || *port > 65535 {
		return Config{}, fmt.Errorf("invalid port %d", *port)
	}
	if *dataDir == "" && defaultErr != nil {
		return Config{}, defaultErr
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
		JevConfigPath:   *jevConfig,
		FaceModelDir:    *faceModels,
		ONNXRuntimeLib:  *onnxRuntime,
	}, nil
}

// flagSet reports whether the flag name was given on the command line.
func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
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

// envInt is the integer in the environment variable key, fallback when it is unset. A value that
// is not an integer is an error: silently using the default would start the server somewhere the
// operator did not ask for.
func envInt(getenv func(string) string, key string, fallback int) (int, error) {
	v := getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: not an integer", key, v)
	}
	return n, nil
}

// envBool is the boolean in the environment variable key: 1, true, yes, on are true and unset, empty,
// 0, false, no, off are false (case-insensitive). Any other value is an error: "MAM_NO_BROWSER=0"
// must not count as set, and a typo must not silently pick a side.
func envBool(getenv func(string) string, key string) (bool, error) {
	v := getenv(key)
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	}
	return false, fmt.Errorf("invalid %s %q: not a boolean (1/true/yes/on or 0/false/no/off)", key, v)
}
