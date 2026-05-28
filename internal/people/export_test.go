package people

// Test-only re-exports of unexported helpers, so the external _test package can
// drive the job wire encoding without making it part of the public API.

// EncodeDetectedForTest exposes encodeDetected for the serve-side ResultHandler
// test (which needs to construct job Data without going through a worker).
func EncodeDetectedForTest(hash string, detected []DetectedFace) map[string]any {
	return encodeDetected(hash, detected)
}
