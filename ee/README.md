# Enterprise license

This file governs every `ee/` directory in this repository: the parts of Beskos that require a
Pro license key to run -- see
[docs/OPEN-SOURCE.md §3.2](../docs/OPEN-SOURCE.md#32-distribuição-das-funcionalidades-proposta-a-validar)
for which features that is. Its terms are in [LICENSE](LICENSE), separate from the
[repository's](../LICENSE.md).

The code itself lives where it is built:

```
backend/ee/           Go: //go:build pro -- see backend/ee/README.md
frontend/src/ee/       React: mounted only when GET /api/edition reports the capability
```

A backend build without `-tags pro` compiles none of `backend/ee/`; a frontend build always
ships `frontend/src/ee/`'s code (same bundle for every edition, per docs/OPEN-SOURCE.md §3.3),
gated at render time by the capability the API reports.
