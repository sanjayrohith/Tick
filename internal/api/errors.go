package api

import (
	"errors"
	"net/http"

	"github.com/sanjayrohith/tick/internal/domain"
)

// apiErrorBody is the response body every error path writes:
// {"error":{"code","message","fields"}}. fields is present only for a
// validation failure.
type apiErrorBody struct {
	Error apiErrorDetail `json:"error"`
}

type apiErrorDetail struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Fields  []fieldError `json:"fields,omitempty"`
}

// writeError writes the unified error envelope. It is the only function in
// this package that writes an error response body, so every handler produces
// the same shape.
func writeError(w http.ResponseWriter, status int, code, message string, fields []fieldError) {
	writeJSON(w, status, apiErrorBody{Error: apiErrorDetail{Code: code, Message: message, Fields: fields}})
}

// writeJSONError writes a generic client or server error with no field-level
// detail: a bad request body, an unsupported input, a store failure whose
// specific cause the client cannot act on.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	code := "internal"
	if status < http.StatusInternalServerError {
		code = "invalid_request"
	}
	writeError(w, status, code, message, nil)
}

// writeValidationError responds with every field-level problem found, so a
// client can fix a bad submission in one round trip.
func writeValidationError(w http.ResponseWriter, errs []fieldError) {
	writeError(w, http.StatusBadRequest, "invalid_request", "request failed validation", errs)
}

// sentinelError maps one domain sentinel error to the status, code and
// client-safe message it produces. Keeping the mapping as data in one place
// is what stops a driver-level error message -- which may embed SQL text --
// from ever reaching a client: every case here supplies its own static
// message instead of forwarding err.Error().
type sentinelError struct {
	err     error
	status  int
	code    string
	message string
}

var sentinelErrors = []sentinelError{
	{domain.ErrNotFound, http.StatusNotFound, "not_found", "task not found"},
	{domain.ErrNotCancellable, http.StatusConflict, "not_cancellable",
		"task is running or already terminal and cannot be cancelled"},
}

// writeStoreError maps err to the response the codebase has agreed a client
// should see. A sentinel from the domain package gets its dedicated status
// and message; anything else -- a dropped connection, a constraint violation,
// a context deadline -- is a 500 with a generic message, never the
// driver-level error text itself.
func writeStoreError(w http.ResponseWriter, err error, fallbackMessage string) {
	for _, se := range sentinelErrors {
		if errors.Is(err, se.err) {
			writeError(w, se.status, se.code, se.message, nil)
			return
		}
	}
	writeError(w, http.StatusInternalServerError, "internal", fallbackMessage, nil)
}
