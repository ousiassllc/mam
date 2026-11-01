module github.com/ousiassllc/mam

go 1.26.0

toolchain go1.26.10

require (
	github.com/a-h/templ v0.3.1100
	github.com/bodgit/sevenzip v1.6.0
	github.com/coder/websocket v1.8.15
	github.com/danielgtaylor/huma/v2 v2.39.1
	github.com/gin-gonic/gin v1.12.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/pressly/goose/v3 v3.29.0
	github.com/shota3506/onnxruntime-purego v0.0.0-20260315223538-8db8bd7424b2
	github.com/stretchr/testify v1.12.1
	github.com/wailsapp/wails/v3 v3.0.0-beta.34
	github.com/zalando/go-keyring v0.2.8
	golang.org/x/image v0.47.0
	golang.org/x/mod v0.41.0
	golang.org/x/sys v0.49.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
	modernc.org/sqlite v1.60.1
)

require (
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/andybalholm/brotli v1.2.7 // indirect
	github.com/bodgit/plumbing v1.3.0 // indirect
	github.com/bodgit/windows v1.0.1 // indirect
	github.com/bytedance/gopkg v0.1.4 // indirect
	github.com/bytedance/sonic v1.15.2 // indirect
	github.com/bytedance/sonic/loader v0.5.1 // indirect
	github.com/cloudwego/base64x v0.1.7 // indirect
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/ebitengine/purego v0.9.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/gin-contrib/sse v1.1.1 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.3 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.20.1 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/pelletier/go-toml/v2 v2.4.3 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/quic-go/quic-go v0.60.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/sethvargo/go-retry v0.5.0 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.3.1 // indirect
	github.com/ulikunitz/xz v0.5.15 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.mongodb.org/mongo-driver/v2 v2.8.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	go4.org v0.0.0-20200411211856-f5505b9728dd // indirect
	golang.org/x/arch v0.29.0 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	modernc.org/libc v1.78.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

// onnxruntime-purego (face identity similarity, backend/internal/facesim) does not build for Windows upstream: purego.Dlopen
// is Unix-only, and ORT takes the model path as UTF-16 there. Its open PR #17 (https://github.com/shota3506/onnxruntime-purego/pull/17,
// the only commit 9c3f4286f91e of aubs3993's fork on top of the pinned upstream commit 8db8bd7424b2) loads the library with
// syscall.LoadLibrary and encodes the path as UTF-16; everything else is unchanged. Drop this replace when upstream releases
// Windows support (spec/architecture/model-adapters.md §10.1).
replace github.com/shota3506/onnxruntime-purego => github.com/aubs3993/onnxruntime-purego v0.0.0-20260728144648-9c3f4286f91e
