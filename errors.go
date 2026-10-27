package locationsdk

// ValidationError is a rejected configuration or input, not a transport outage.
// Use errors.As to preserve the distinction without importing gRPC.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
