# onnxruntime-purego (vendored copy)

This directory is a copy of the Go bindings `github.com/shota3506/onnxruntime-purego` (MIT, see `LICENSE`), taken in as part of
the MAM source tree (Issue #949). `go.mod` of MAM points at it with `replace github.com/shota3506/onnxruntime-purego => ./third_party/onnxruntime-purego`.

- Upstream: https://github.com/shota3506/onnxruntime-purego, commit `8db8bd7424b2` (2026-03-15), the one MAM used before.
- What differs from upstream: the contents of upstream's open pull request #17 (https://github.com/shota3506/onnxruntime-purego/pull/17,
  commit `9c3f4286f91e` of the fork `aubs3993/onnxruntime-purego`, which MAM used through a `replace` until this copy). It makes the
  package build for Windows: `loader_unix.go` / `loader_windows.go` load the library with `purego.Dlopen` / `syscall.LoadLibrary`, and
  `modelpath_unix.go` / `modelpath_windows.go` pass the model path as UTF-16 on Windows. Nothing else was changed.
- What was left out: tests, the test data, the `genai` package, the code generator and the build files of upstream (MAM imports only
  `onnxruntime`). Files were copied byte for byte.
- Removing it: when upstream releases Windows support, delete this directory and the `replace` line of `go.mod`, and require the
  released version (spec/architecture/model-adapters.md §10.1).
