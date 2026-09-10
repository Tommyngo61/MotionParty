package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	proto "github.com/MeterHome/Meter-Node/proto"
	"github.com/google/uuid"

	"github.com/MeterHome/Meter-Node/console/internal/enroll"
	"github.com/MeterHome/Meter-Node/console/internal/model"
)

// maxEnrollBody bounds the enrollment request.
//
// The endpoint is unauthenticated by necessity — a node has no credential yet
// — so it is the one place an unauthenticated caller can make the controller
// do work. A hardware inventory with a dozen disks and eight GPUs is a few
// kilobytes; 64 KiB is generous and still cheap to reject.
const maxEnrollBody = 64 << 10

// handleEnroll exchanges a one-time token for a node credential.
func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxEnrollBody+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, proto.ErrCodeBadRequest, "Could not read the request body.", nil)
		return
	}
	if len(body) > maxEnrollBody {
		writeErr(w, http.StatusRequestEntityTooLarge, proto.ErrCodeBadRequest,
			"Enrollment request is too large.", map[string]string{"max_bytes": strconv.Itoa(maxEnrollBody)})
		return
	}

	var req proto.EnrollRequest
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		// An unknown field usually means an agent newer than this controller,
		// which is a rollout ordering bug worth naming precisely rather than
		// letting it read as a malformed request.
		writeErr(w, http.StatusBadRequest, proto.ErrCodeBadRequest,
			"Could not parse the enrollment request. If this agent is newer than the controller, upgrade the controller first.",
			map[string]string{"parse_error": err.Error()})
		return
	}

	ip := clientIP(r)
	res, err := s.enroll.Enroll(r.Context(), &req, ip)
	if err != nil {
		s.writeEnrollError(w, r, &req, err, ip)
		return
	}

	hints := s.enroll.ConfigHints()
	hints.StreamURL = s.cfg.StreamURL()
	hints.TelemetryURL = s.cfg.TelemetryURL()

	resp := proto.EnrollResponse{
		NodeID:         res.NodeID.String(),
		Credential:     res.Credential,
		ControllerKeys: s.enroll.ControllerKeys(),
		Config:         hints,
		ServerTimeMS:   time.Now().UnixMilli(),
	}
	if res.SiteID != nil {
		resp.SiteID = res.SiteID.String()
	}

	// Log the clock skew we observed. A node that enrolls hours off is worth
	// knowing about before its first sample lands in a hypertable with a
	// timestamp from last Tuesday.
	if req.SentAtMS > 0 {
		skew := resp.ServerTimeMS - req.SentAtMS
		if skew < -60_000 || skew > 60_000 {
			s.log.Warn("node enrolled with significant clock skew",
				"node_id", res.NodeID, "skew_ms", skew, "request_id", RequestID(r.Context()))
		}
	}

	s.log.Info("node enrolled",
		"node_id", res.NodeID, "re_enrolled", res.ReEnrolled,
		"agent_version", req.AgentVersion, "remote", ip,
		"request_id", RequestID(r.Context()))

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) writeEnrollError(w http.ResponseWriter, r *http.Request, req *proto.EnrollRequest, err error, ip string) {
	status, code, msg := enrollErrorStatus(err)

	var re *enroll.ReattestationError
	if errors.As(err, &re) {
		s.log.Warn("enrollment blocked on re-attestation",
			"node_id", re.NodeID, "reattestation_id", re.ReattestationID,
			"changed_fields", strings.Join(re.ChangedFields, ","),
			"remote", ip, "request_id", RequestID(r.Context()))
		writeJSON(w, status, proto.ReattestationRequired{
			NodeID:          re.NodeID.String(),
			ChangedFields:   re.ChangedFields,
			BoundHash:       re.BoundHash,
			PresentedHash:   re.PresentedHash,
			ReattestationID: re.ReattestationID.String(),
			Message:         msg,
		})
		return
	}

	if status >= 500 {
		s.log.Error("enrollment failed", "error", err, "remote", ip, "request_id", RequestID(r.Context()))
	} else {
		// Never log the token itself, only what an operator needs to correlate
		// a failed install with a minted token.
		s.log.Warn("enrollment refused",
			"code", code, "agent_version", req.AgentVersion,
			"remote", ip, "request_id", RequestID(r.Context()))
	}
	writeErr(w, status, code, msg, nil)
}

// --- Operator: enrollment tokens ----------------------------------------

type mintTokenRequest struct {
	Label        string `json:"label"`
	SiteID       string `json:"site_id,omitempty"`
	NodeNameHint string `json:"node_name_hint,omitempty"`
	TTLHours     int    `json:"ttl_hours,omitempty"`
}

type mintTokenResponse struct {
	ID     string `json:"id"`
	Prefix string `json:"token_prefix"`
	// Token is returned exactly once. It is not stored and cannot be
	// recovered; losing it costs one re-mint.
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	Warning   string `json:"warning"`
}

func (s *Server) handleMintToken(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())

	var req mintTokenRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, proto.ErrCodeBadRequest, "Could not parse the request.", nil)
		return
	}

	params := enroll.MintParams{
		Label: req.Label, NodeNameHint: req.NodeNameHint,
		Actor: p.Name, ActorRole: string(p.Role),
		RequestID: RequestID(r.Context()), RemoteAddr: clientIP(r),
	}
	if req.SiteID != "" {
		id, err := uuid.Parse(req.SiteID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, proto.ErrCodeBadRequest, "site_id must be a UUID.", nil)
			return
		}
		params.SiteID = &id
	}
	if req.TTLHours > 0 {
		params.TTL = time.Duration(req.TTLHours) * time.Hour
	}

	res, err := s.enroll.MintToken(r.Context(), params)
	if err != nil {
		if errors.Is(err, enroll.ErrBadRequest) {
			writeErr(w, http.StatusBadRequest, proto.ErrCodeBadRequest, err.Error(), nil)
			return
		}
		s.log.Error("mint enrollment token", "error", err, "actor", p.Name)
		writeErr(w, http.StatusInternalServerError, proto.ErrCodeInternal, "Could not mint a token.", nil)
		return
	}

	writeJSON(w, http.StatusCreated, mintTokenResponse{
		ID:        res.Token.ID.String(),
		Prefix:    res.Token.TokenPrefix,
		Token:     res.Secret,
		ExpiresAt: res.Token.ExpiresAt.UTC().Format(time.RFC3339),
		Warning:   "This token is shown once and is not recoverable. It is single-use and expires at the time above.",
	})
}

type tokenView struct {
	ID           string `json:"id"`
	Prefix       string `json:"token_prefix"`
	Label        string `json:"label,omitempty"`
	SiteID       string `json:"site_id,omitempty"`
	NodeNameHint string `json:"node_name_hint,omitempty"`
	CreatedBy    string `json:"created_by"`
	CreatedAt    string `json:"created_at"`
	ExpiresAt    string `json:"expires_at"`
	State        string `json:"state"`
	ConsumedAt   string `json:"consumed_at,omitempty"`
	ConsumedBy   string `json:"consumed_by_node,omitempty"`
	RevokedAt    string `json:"revoked_at,omitempty"`
	RevokeReason string `json:"revoke_reason,omitempty"`
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	f := enroll.TokenFilter{OnlyOpen: r.URL.Query().Get("state") == "open"}
	if v := r.URL.Query().Get("site_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, proto.ErrCodeBadRequest, "site_id must be a UUID.", nil)
			return
		}
		f.SiteID = &id
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Limit = n
		}
	}

	tokens, err := s.enroll.ListTokens(r.Context(), f)
	if err != nil {
		s.log.Error("list enrollment tokens", "error", err)
		writeErr(w, http.StatusInternalServerError, proto.ErrCodeInternal, "Could not list tokens.", nil)
		return
	}

	now := time.Now()
	out := make([]tokenView, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, tokenViewOf(t, now))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

func tokenViewOf(t model.Token, now time.Time) tokenView {
	v := tokenView{
		ID: t.ID.String(), Prefix: t.TokenPrefix, Label: t.Label,
		NodeNameHint: t.NodeNameHint, CreatedBy: t.CreatedBy,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: t.ExpiresAt.UTC().Format(time.RFC3339),
		State:     tokenState(t, now), RevokeReason: t.RevokeReason,
	}
	if t.SiteID != nil {
		v.SiteID = t.SiteID.String()
	}
	if t.ConsumedAt != nil {
		v.ConsumedAt = t.ConsumedAt.UTC().Format(time.RFC3339)
	}
	if t.ConsumedByNode != nil {
		v.ConsumedBy = t.ConsumedByNode.String()
	}
	if t.RevokedAt != nil {
		v.RevokedAt = t.RevokedAt.UTC().Format(time.RFC3339)
	}
	return v
}

// tokenState collapses the three nullable timestamps into the one word an
// operator scanning a list actually wants.
func tokenState(t model.Token, now time.Time) string {
	switch {
	case t.RevokedAt != nil:
		return "revoked"
	case t.ConsumedAt != nil:
		return "consumed"
	case !now.Before(t.ExpiresAt):
		return "expired"
	default:
		return "open"
	}
}

type revokeTokenRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())

	id, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, proto.ErrCodeBadRequest, "Token id must be a UUID.", nil)
		return
	}
	var req revokeTokenRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, proto.ErrCodeBadRequest, "Could not parse the request.", nil)
		return
	}

	if err := s.enroll.RevokeToken(r.Context(), id, p.Name, string(p.Role), req.Reason,
		RequestID(r.Context()), clientIP(r)); err != nil {
		if errors.Is(err, enroll.ErrTokenInvalid) {
			writeErr(w, http.StatusNotFound, proto.ErrCodeTokenInvalid, "No such token.", nil)
			return
		}
		s.log.Error("revoke enrollment token", "error", err, "token_id", id, "actor", p.Name)
		writeErr(w, http.StatusInternalServerError, proto.ErrCodeInternal, "Could not revoke the token.", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked", "id": id.String()})
}

func (s *Server) handleWhoami(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{"name": p.Name, "role": string(p.Role)})
}

// timeoutCtx derives a bounded context from a request.
func timeoutCtx(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
