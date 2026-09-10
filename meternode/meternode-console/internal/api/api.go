// Package api is the controller's HTTP surface: the agent-facing endpoints and
// the operator API behind them.
//
// Two audiences share one router, and they are authenticated differently. The
// agent-facing routes authenticate a node — with a one-time token at
// enrollment, and with a signed credential afterwards. The operator routes
// authenticate a person or a service token and gate on a role. Nothing that
// takes an operator's authority is reachable with a node's credential, or the
// other way round.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	proto "github.com/meterhome/meternode-proto"

	"github.com/meterhome/meternode-console/internal/config"
	"github.com/meterhome/meternode-console/internal/enroll"
	"github.com/meterhome/meternode-console/internal/model"
)

// Health reports whether the controller's dependencies are usable.
type Health interface {
	Ping(ctx context.Context) error
}

// Server holds the controller's HTTP dependencies.
type Server struct {
	cfg        *config.Config
	log        *slog.Logger
	enroll     *enroll.Service
	principals PrincipalStore
	health     Health
	version    string
	startedAt  time.Time
}

// Options configure a Server.
type Options struct {
	Config     *config.Config
	Logger     *slog.Logger
	Enroll     *enroll.Service
	Principals PrincipalStore
	Health     Health
	Version    string
}

// New builds a Server.
func New(o Options) *Server {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		cfg: o.Config, log: log, enroll: o.Enroll,
		principals: o.Principals, health: o.Health,
		version: o.Version, startedAt: time.Now(),
	}
}

// Routes builds the router.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	r.Use(requestIDMiddleware)
	r.Use(recoverMiddleware(s.log))
	r.Use(loggingMiddleware(s.log))
	// Agents on a residential link can be slow; the read timeout that matters
	// is on the server, not here. This one only bounds a wedged handler.
	r.Use(middleware.Timeout(30 * time.Second))

	// --- Unauthenticated -------------------------------------------------
	// /healthz and /readyz are for the orchestrator, and must not require a
	// credential or the orchestrator cannot use them.
	r.Get("/healthz", s.handleLive)
	r.Get("/readyz", s.handleReady)
	r.Get("/v1/schema", s.handleSchema)

	// --- Agent-facing ----------------------------------------------------
	// Enrollment is necessarily open: a node has no credential yet. Its
	// authentication IS the one-time token in the body.
	r.Post(proto.EnrollPath, s.handleEnroll)

	// --- Operator API ----------------------------------------------------
	r.Route("/v1/admin", func(r chi.Router) {
		r.Use(s.authMiddleware)

		r.Get("/whoami", s.handleWhoami)

		r.Route("/enrollment-tokens", func(r chi.Router) {
			r.With(requireRole(model.RoleViewer)).Get("/", s.handleListTokens)
			// Minting a token creates the one credential that exists before a
			// node has an identity, so it takes operator, not viewer.
			r.With(requireRole(model.RoleOperator)).Post("/", s.handleMintToken)
			r.With(requireRole(model.RoleOperator)).Post("/{id}/revoke", s.handleRevokeToken)
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "not_found", "No such endpoint.", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "That method is not allowed here.", nil)
	})

	return r
}
