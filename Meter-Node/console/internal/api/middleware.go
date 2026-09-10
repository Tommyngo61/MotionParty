package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	proto "github.com/MeterHome/Meter-Node/proto"
	"github.com/google/uuid"

	"github.com/MeterHome/Meter-Node/console/internal/model"
)

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxPrincipal
)

// Principal is an authenticated caller: an operator, or a service token.
type Principal struct {
	Name string // email for a human, token name for a service
	Role model.OperatorRole
}

// PrincipalStore resolves a bearer token to a Principal.
//
// It is an interface so the OIDC path can be added beside it without changing
// any handler: phase 1 authenticates service tokens, phase 2 authenticates
// people, and both produce the same Principal.
type PrincipalStore interface {
	PrincipalByTokenHash(ctx context.Context, hash []byte) (*Principal, error)
}

// RequestID returns the request id attached by the middleware.
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(ctxRequestID).(string)
	return v
}

// PrincipalFrom returns the authenticated caller, if any.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxPrincipal).(Principal)
	return p, ok
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxRequestID, id)))
	})
}

// statusRecorder captures the response status for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func loggingMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}
			log.Log(r.Context(), level, "http request",
				"method", r.Method, "path", r.URL.Path, "status", rec.status,
				"bytes", rec.bytes, "duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestID(r.Context()), "remote", clientIP(r))
		})
	}
}

// recoverMiddleware turns a panic in one handler into a 500 for that request
// instead of taking the process down.
//
// The controller holds thousands of live agent sockets. A panic that kills the
// process drops every one of them, and they all reconnect at once — which is
// the reconnect storm the whole transport design exists to avoid.
func recoverMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					log.Error("panic in handler",
						"error", v, "path", r.URL.Path,
						"request_id", RequestID(r.Context()),
						"stack", string(debug.Stack()))
					writeErr(w, http.StatusInternalServerError, proto.ErrCodeInternal,
						"The controller hit an unexpected error.", nil)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// authMiddleware authenticates a bearer token and attaches a Principal.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		if raw == "" {
			writeErr(w, http.StatusUnauthorized, proto.ErrCodeUnauthorized, "Authentication required.", nil)
			return
		}
		sum := sha256.Sum256([]byte(raw))
		p, err := s.principals.PrincipalByTokenHash(r.Context(), sum[:])
		if err != nil {
			s.log.Error("resolve principal", "error", err, "request_id", RequestID(r.Context()))
			writeErr(w, http.StatusInternalServerError, proto.ErrCodeInternal, "Could not verify credentials.", nil)
			return
		}
		if p == nil {
			// Constant-time compare against a dummy so a miss and a hit cost
			// roughly the same. The token is hashed either way, so this is
			// belt and braces rather than the primary defence.
			subtle.ConstantTimeCompare(sum[:], make([]byte, sha256.Size))
			writeErr(w, http.StatusUnauthorized, proto.ErrCodeUnauthorized, "Invalid credentials.", nil)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxPrincipal, *p)))
	})
}

// requireRole gates a route on a minimum role.
func requireRole(want model.OperatorRole) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFrom(r.Context())
			if !ok || !p.Role.AtLeast(want) {
				writeErr(w, http.StatusForbidden, proto.ErrCodeUnauthorized,
					"This action requires the "+string(want)+" role.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if v, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// clientIP prefers the proxy-supplied address, since the controller sits behind
// a TLS terminator in every deployment that matters.
func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if first, _, ok := strings.Cut(v, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(v)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
