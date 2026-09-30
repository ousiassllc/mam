package facesim

// Default file names of the models, looked up in ONNXConfig.ModelDir.
const (
	DetectorFile   = "face_detection_yunet_2023mar.onnx"
	RecognizerFile = "face_recognition_sface_2021dec.onnx"
)

// ONNXConfig locates the models and the ONNX Runtime library.
type ONNXConfig struct {
	// ModelDir holds DetectorFile and RecognizerFile.
	ModelDir string
	// DetectorPath / RecognizerPath override the default file names when set.
	DetectorPath   string
	RecognizerPath string
	// RuntimeLibrary is the ONNX Runtime shared library (libonnxruntime.so, .dylib or onnxruntime.dll,
	// version 1.23.x). Empty lets the system search its standard library locations.
	RuntimeLibrary string
	// MinScore is the detection confidence a face needs (YuNet score, 0..1).
	MinScore float32
}
