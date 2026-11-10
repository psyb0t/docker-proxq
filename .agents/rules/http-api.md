# HTTP API

Prefix: API. Routes are built in `buildRoutes` in `internal/app/app.go`; handlers live in `internal/proxy/`.

- **API1:** `X-Proxq-Source: proxq` is how clients, including `pkg/clients/openai`, tell a proxq answer from a replayed upstream answer. Every response proxq writes on the request path (`202`, `400`, `502`, `500`, `307`) and every `404` from the jobs API sets it through `setProxqSourceHeader`. A replayed upstream response from `/content` must never carry it.
- **API2:** The jobs API shape is public: `{"jobId": ...}` on submit, `id`, `status`, `error`, `completedAt` on status, `{"status": "cancelled"}` on cancel. Status values come from `StatusFromTaskState` in `internal/proxy/job.go`. Renaming a field or a status breaks `pkg/clients/openai` and every caller.
- **API3:** Bypass rules live in `ServeHTTP` and `shouldBypassQueue` in `internal/proxy/handler.go`. A change to them updates `docs/routing.md` and the skill.
- **API4:** An API change updates `docs/api.md`, `.agents/skills/proxq/SKILL.md` and, when the client depends on it, `pkg/clients/openai` and `docs/openai-client.md` in the same change.
