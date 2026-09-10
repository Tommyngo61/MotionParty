package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// chiURLParam is a one-line indirection so handlers do not each import chi
// only to read a path parameter.
func chiURLParam(r *http.Request, key string) string { return chi.URLParam(r, key) }
