package mqtt

// ErrUnsupported is returned when an endpoint receives a message on an
// unrecognized topic.
var ErrUnsupported = NewRpcError("unsupported operation")

func NewRpcError(msg string) error {
	return &rpcError{msg: msg}
}

type rpcError struct {
	msg string
}

func (e *rpcError) Error() string {
	return e.msg
}

func (e *rpcError) Is(err error) bool {
	// These errors get sent across the wire as plain strings
	return err.Error() == e.msg
}
