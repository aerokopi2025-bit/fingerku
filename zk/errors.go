package zk

import (
	"errors"
	"fmt"
)

var (
	// ErrNotConnected is returned when an operation is attempted without an active connection.
	ErrNotConnected = errors.New("zk: instance is not connected")

	// ErrUnauthorized is returned when device authentication fails.
	ErrUnauthorized = errors.New("zk: authentication failed or connection unauthorized")

	// ErrDeviceUnreachable is returned when ping/socket connection to the device fails.
	ErrDeviceUnreachable = errors.New("zk: device is unreachable")

	// ErrInvalidTcpHeader is returned when an invalid TCP header signature is received.
	ErrInvalidTcpHeader = errors.New("zk: invalid TCP packet header received")

	// ErrBufferEmpty is returned when an empty buffer is received.
	ErrBufferEmpty = errors.New("zk: empty data buffer received")
)

// ResponseError represents an error returned by the device response code.
type ResponseError struct {
	Message string
	Code    uint16
}

func (e *ResponseError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("zk: %s (code: %d / 0x%04X)", e.Message, e.Code, e.Code)
	}
	return fmt.Sprintf("zk: %s", e.Message)
}

// NewResponseError creates a new ResponseError.
func NewResponseError(msg string, code uint16) error {
	return &ResponseError{
		Message: msg,
		Code:    code,
	}
}

// NetworkError wraps underlying network socket errors.
type NetworkError struct {
	Op  string
	Err error
}

func (e *NetworkError) Error() string {
	return fmt.Sprintf("zk network error during %s: %v", e.Op, e.Err)
}

func (e *NetworkError) Unwrap() error {
	return e.Err
}
