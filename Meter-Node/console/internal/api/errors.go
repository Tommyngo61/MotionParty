package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	proto "github.com/MeterHome/Meter-Node/proto"

	"github.com/MeterHome/Meter-Node/console/internal/enroll"
)

// writeJSON renders v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Nothing this API returns should ever be cached by an intermediary: the
	// fleet view is live data, and enrollment responses contain credentials.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response", "error", err)
	}
}

// writeErr renders the uniform APIError body.
func writeErr(w http.ResponseWriter, status int, code, msg string, detail map[string]string) {
	writeJSON(w, status, &proto.APIError{Code: code, Message: msg, Detail: detail})
}

// enrollErrorStatus maps a service error onto an HTTP status and a stable API
// error code.
//
// The messages are written for a field tech reading a terminal in someone's
// utility room, not for a developer reading a log: each one says what to do
// next. The status codes matter too — the agent retries a 5xx with backoff and
// gives up on a 4xx, so misclassifying a spent token as a server error would
// have a node hammering the controller forever.
func enrollErrorStatus(err error) (int, string, string) {
	var re *enroll.ReattestationError
	switch {
	case errors.As(err, &re):
		return http.StatusConflict, proto.ErrCodeReattestRequired,
			"This node's hardware no longer matches what its identity is bound to. An operator must review the change before it can rejoin."

	case errors.Is(err, enroll.ErrTokenInvalid):
		return http.StatusUnauthorized, proto.ErrCodeTokenInvalid,
			"That enrollment token is not recognised. Check for a typo, then ask for a new one."
	case errors.Is(err, enroll.ErrTokenExpired):
		return http.StatusUnauthorized, proto.ErrCodeTokenExpired,
			"That enrollment token has expired. Ask for a new one."
	case errors.Is(err, enroll.ErrTokenConsumed):
		return http.StatusConflict, proto.ErrCodeTokenConsumed,
			"That enrollment token has already been used. Tokens are single-use; ask for a new one."
	case errors.Is(err, enroll.ErrTokenRevoked):
		return http.StatusUnauthorized, proto.ErrCodeTokenRevoked,
			"That enrollment token was revoked. Ask for a new one."

	case errors.Is(err, enroll.ErrNodeQuarantined):
		return http.StatusForbidden, proto.ErrCodeNodeQuarantined,
			"This node is quarantined and cannot rejoin until an operator releases it."
	case errors.Is(err, enroll.ErrNodeRevoked):
		return http.StatusForbidden, proto.ErrCodeNodeRevoked,
			"This node has been retired and cannot rejoin."
	case errors.Is(err, enroll.ErrFingerprintConflict):
		return http.StatusConflict, proto.ErrCodeFingerprintConflict,
			"This hardware is already bound to a different node."

	case errors.Is(err, enroll.ErrBadRequest):
		return http.StatusBadRequest, proto.ErrCodeBadRequest, err.Error()

	default:
		// Deliberately vague: an unexpected failure here must not describe the
		// controller's internals to an endpoint that is, by necessity,
		// reachable without authentication.
		return http.StatusInternalServerError, proto.ErrCodeInternal,
			"The controller could not complete enrollment. This is a server-side problem; the agent will retry."
	}
}
