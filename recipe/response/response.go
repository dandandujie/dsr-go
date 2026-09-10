// Package response holds protocol response construction and event
// accumulation.
package response

import (
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/request"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// ProtocolResponse is a protocol response that can accumulate events from its
// chunk generator.
type ProtocolResponse interface {
	// Append accumulates one event produced by this protocol's generator.
	Append(event stream.Event)
	// DoneMessage returns the final transport sentinel if the protocol requires
	// one.
	DoneMessage() (string, bool)
}

// ErrorDetail is one error object.
type ErrorDetail struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    string  `json:"code"`
}

// ErrorResponse is the error body returned for a failed conversion.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ConversionResponse builds the error response body for a conversion error.
func ConversionResponse(err *request.ConversionError) ErrorResponse {
	statusCode := err.StatusCode()
	errorType := "invalid_request_error"
	if err.Kind == request.ConversionInternal {
		errorType = "internal_error"
	}
	code := "invalid_request_error"
	if statusCode >= 500 {
		code = errorType
	}
	return ErrorResponse{Error: ErrorDetail{
		Message: err.Detail,
		Type:    errorType,
		Param:   nil,
		Code:    code,
	}}
}

// JSON renders the error response body.
func (r ErrorResponse) JSON() (string, error) {
	return jsonx.MarshalCompactString(r)
}
