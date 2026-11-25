package cache

// Error preserves a failure stage/category without exposing URLs or response
// bodies. The wrapped cause remains available for retry hints and cancellation.
type Error struct {
	Stage, Code string
	Err         error
}

func (e *Error) Error() string { return e.Stage + ": " + e.Code }
func (e *Error) Unwrap() error { return e.Err }
