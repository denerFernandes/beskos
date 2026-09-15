//go:build pro

package ee

import (
	"github.com/go-chi/chi/v5"

	"mailin/backend/internal/edition"
)

// Register wires the Enterprise routes for whatever caps grants. No feature has been moved
// behind this seam yet -- see the F2 status in docs/OPEN-SOURCE.md for what is still pending.
func Register(r chi.Router, caps edition.Capabilities, deps Deps) {}
