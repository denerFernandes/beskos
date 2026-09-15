// Package ee holds the Beskos Enterprise features: the capabilities a Pro license key unlocks
// (see internal/edition and docs/OPEN-SOURCE.md §3.2). It only compiles into the binary when
// built with `-tags pro`; the default build links build_community.go instead, which registers
// nothing. Its license is ../../ee/LICENSE, separate from the repository's.
package ee

import "mailin/backend/internal/db"

// Deps is what an Enterprise route needs from the rest of the server. It grows one field at a
// time, as a feature actually moves into this package -- never speculatively.
type Deps struct {
	Queries *db.Queries
}

// Register wires the Enterprise routes for whatever caps grants. It is defined twice, once per
// build tag (build_pro.go, build_community.go); this file only holds the shared types.
