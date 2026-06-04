//go:build !onnx

package suggest

import "fmt"

// NewOnnxPredictor is unavailable in this build.
// Rebuild with: go build -tags onnx .
// See pkg/suggest/onnx_predictor.go for prerequisites.
func NewOnnxPredictor(_, _ string) (*OnnxPredictor, error) {
	return nil, fmt.Errorf(
		"ONNX engine not compiled in — rebuild with: go build -tags onnx .\n" +
			"See pkg/suggest/onnx_predictor.go for setup instructions")
}

// OnnxPredictor is a placeholder type for non-onnx builds.
// The real implementation lives in onnx_predictor.go (build tag: onnx).
type OnnxPredictor struct{}

func (*OnnxPredictor) PredictNextContext(_ []string) []string { return nil }
func (*OnnxPredictor) Starters() []string                    { return nil }
func (*OnnxPredictor) Close() error                          { return nil }
