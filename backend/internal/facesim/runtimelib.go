package facesim

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// WindowsRuntimeLibraryFile is the ONNX Runtime DLL the Windows installer puts next to mam.exe
// (scripts/fetch-onnxruntime.sh, build/windows/nsis/project.nsi).
const WindowsRuntimeLibraryFile = "onnxruntime.dll"

// RuntimeLibraryPath is the ONNX Runtime library to load. configured (--onnxruntime-lib) wins. Without it,
// Windows takes the DLL next to the executable exe and never the Windows DLL search order: System32 can hold
// an older onnxruntime.dll (Windows ML) that does not speak the C API version the bindings use. Other systems
// return "", which leaves the library to the system's standard library search.
// It fails when Windows has neither configured nor exe.
func RuntimeLibraryPath(goos, configured, exe string) (string, error) {
	switch {
	case configured != "":
		return configured, nil
	case goos != "windows":
		return "", nil
	case exe == "":
		return "", errors.New("the location of mam.exe is unknown, so " + WindowsRuntimeLibraryFile + " next to it cannot be found (set --onnxruntime-lib)")
	}
	return filepath.Join(filepath.Dir(exe), WindowsRuntimeLibraryFile), nil
}

// checkRuntimeLibrary names a library given by path (not a bare name left to the system search) that is
// not there, before the loader reports it as "module not found".
func checkRuntimeLibrary(lib string) error {
	if lib == "" || filepath.Base(lib) == lib {
		return nil
	}
	if _, err := os.Stat(lib); err != nil {
		return fmt.Errorf("ONNX Runtime library: %w", err)
	}
	return nil
}

// loadFailureHint is what to try when the library file is there but does not load. On Windows the usual cause
// is the missing Microsoft Visual C++ Redistributable that onnxruntime.dll links against.
func loadFailureHint() string {
	if runtime.GOOS == "windows" {
		return " (" + WindowsRuntimeLibraryFile + " needs the Microsoft Visual C++ 2015-2022 Redistributable (x64))"
	}
	return ""
}
