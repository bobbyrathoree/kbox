# kbox Technical Critique: Deep Dive

This report provides a granular look at the `kbox` codebase, focusing on implementation details that impact its "production-grade" status.

## 1. Apply Engine & Convergence Logic
**Status:** Functional but Brittle.

### Findings:
- **Redundant State Checks:** `applyObject` performs a `Get` request before every `Patch`.
    - *Impact:* Doubling the API calls to the K8s control plane. Server-Side Apply (SSA) is designed to be idempotent; the `Patch` will handle creation and updates natively.
- **Hardcoded Type Switching:** The `Apply` loop in `internal/apply/engine.go` uses massive switch statements for every resource type.
    - *Impact:* Adding support for new resources (like `ServiceMonitor`, `PrometheusRule`, or `ExternalSecret`) requires modifying the core engine logic in multiple places.
- **Rollout "Zombie" State:** The `WaitForRollout` logic returns an error if a pod is in `ImagePullBackOff`, but the `Deploy` command doesn't "clean up" or mark that release as failed in the history.
    - *Impact:* The cluster is left in a broken state, and `kbox history` doesn't reflect that the release actually failed to start.

## 2. Configuration & Validation
**Status:** Under-specified.

### Findings:
- **Loose Validation:** `internal/config/validate.go` only checks for the existence of fields and basic ranges.
    - *Missing:* It doesn't validate K8s quantities (e.g., "500z" is accepted as CPU but will fail in K8s). It should use `k8s.io/apimachinery/pkg/api/resource`.
- **Environment Overlay Merging:** Overlays currently override top-level fields. However, complex fields like `env` or `volumes` might need "merge" logic rather than "replace" logic (e.g., adding one extra env var in production without redefining the whole list).
- **YAML Fidelity:** Using `sigs.k8s.io/yaml` for writing the configuration strips all user comments.
    - *Impact:* High frustration for users who use comments for documentation or "todo" reminders in their `kbox.yaml`.

## 3. Dependency Management (The "Managed" part)
**Status:** Elegant but narrow.

### Findings:
- **Permission Bottlenecks:** The `securityContext` for Postgres/MySQL is hardcoded to specific UIDs (e.g., 70 for Postgres). While this works for the default images, it breaks if the user tries to use a non-default image or a different storage provider that requires specific GIDs.
- **No Dependency Health Propagation:** If the Postgres dependency fails to start, the main app deployment proceeds anyway and hangs in a CrashLoop.
    - *Suggestion:* Use `initContainers` or a simple "wait-for" check to ensure dependencies are ready before the main container starts.

## 4. Architectural Observations
- **Package Isolation:** Good. `internal/render` is cleanly decoupled from `internal/apply`. This makes it easy to add a `kbox render` command that doesn't need a cluster connection.
- **Error Handling:** Generally good, but many errors are "swallowed" and replaced with friendlier messages. This is great for new users but frustrating for power users.
    - *Suggestion:* Only wrap errors; don't hide the original `Cause`.

## 5. Summary of "Bugs" found:
1.  **ConfigMap Dead-weight:** `RenderConfigMap` generates a ConfigMap that is never actually mounted or used via `envFrom` in the Deployment.
2.  **Release History Desync:** First deploy of a project doesn't save history if the rollout fails, making it impossible to "rollback" to nothing or a previous state easily.
3.  **Shell Ephemeral Cleanup:** Ephemeral containers launched via `kbox shell` are left in the Pod spec forever (until pod restart). This can lead to bloated Pod specs in long-running pods.

## 6. 🔥 Poison Pills & Silent Failures (Ultra-Think Audit)

These are the "dark corners" where `kbox` might fail silently or cause unrecoverable states.

### A. Immutable Field Deadlock (SSA 409)
**Scenario:** A user changes the `storage` size of a dependency or the `selector` of a Service.
- **The Pill:** `internal/apply/engine.go` uses Server-Side Apply but lacks `FieldManager` conflict resolution (`force=true`). 
- **Result:** The K8s API returns a `409 Conflict`. `kbox` will report an error, and *every subsequent deploy* will fail until the user manually deletes the resource. It literally "deadlocks" the deployment pipeline.

### B. The "Orphaned Resource" Leak
**Scenario:** You remove a `port` or a `dependency` from `kbox.yaml`.
- **The Failure:** `kbox` has no "pruning" logic. Since it only applies what's currently rendered, the old `Service` or `StatefulSet` stays in the cluster forever.
- **Result:** Silent resource leakage and potential port conflicts with future apps.

### C. Log Stream Heart-Attack (Scanner Overflow)
**Scenario:** Your app logs a large JSON blob or a massive Java stack trace ( > 64KB).
- **The failure:** `internal/debug/logs.go` uses `bufio.NewScanner` with default buffers.
- **Result:** The scanner hits `token too long` and **silently exits the goroutine**. The user simply stops seeing logs for that pod with zero error message.

### D. Dockerfile "Magic" Guessing Error
**Scenario:** Multi-stage Dockerfile where Stage 1 (builder) has `EXPOSE 80` but the final stage has `EXPOSE 3000`.
- **The Failure:** `parseExposedPort` is a naive string matcher that returns the *first* match.
- **Result:** `kbox up` misconfigures the Service port to 80, leading to "Connection Refused" while the container logs show the app running fine on 3000.

### E. The "Partial Apply" Zombie State
**Scenario:** Your `Secret` fails to apply (e.g., quota issue), but the `Deployment` succeeds.
- **The Failure:** `Apply` loop continues even if early stages fail.
- **Result:** You get a "Deployment ✓" message followed later by a "Rollout failed". The Pods sit in `CreateContainerConfigError` indefinitely. The user is left wondering why the "Deployment" succeeded if it's not actually working.

### F. Scheduling "Death Trap"
**Scenario:** User sets `resources.memoryLimit: 256Mi` but `resources.memory: 512Mi`.
- **The Failure:** `internal/config/validate.go` doesn't check cross-field constraints (Request <= Limit).
- **Result:** The YAML is valid, `kbox` sends it to the API, and the API rejects it with a cryptic error. If `kbox` has "guessed" 2x limits on a large request, it might exceed node capacity silently.
