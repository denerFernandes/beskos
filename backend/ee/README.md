# backend/ee

Enterprise routes, gated by `-tags pro` and by `internal/edition.Capabilities`. See
[../../ee/README.md](../../ee/README.md) for the license and the overall shape, and
[../../docs/editions.md](../../docs/editions.md) for which features this is meant to hold.

Nothing has moved in here yet: `Register` is a no-op in both builds. Moving a feature is, per
feature: extract its handler(s) here, add the matching field to `Deps`, register its route only
when `caps.<Field>` is true, and prove with a test that the route answers `403 edition_required`
(or similar) without the capability. Do this one feature at a time (project law §6.1), not as one
large change.
