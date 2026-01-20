# kbox v1.0 Roadmap

This roadmap outlines the path from the current proof-of-concept to a production-grade 1.0 release.

## Milestone 1: Stability & Fidelity ( The "Bun" Polish)
*Focus: Ensuring the tool is reliable and doesn't stand in the user's way.*

- [ ] **YAML Comment Preservation:** Switch to `gopkg.in/yaml.v3` and implement a Node-based unmarshaler to keep user comments intact during `kbox add/remove`.
- [ ] **Robust Validation:** Use `k8s.io/apimachinery` to validate all resources (CPU/Memory strings, Names, Ports) before they hit the cluster.
- [ ] **SSA "Force" Apply:** Implement `Force: true` in `PatchOptions` to handle FieldManager conflicts and immutable field deadlocks.
- [ ] **Resource Pruning (Garbage Collection):** Track created resources and delete orphans that are no longer in the `render.Bundle`.
- [ ] **Failed Rollout Recovery:**
    - Save release history *before* the rollout starts.
    - If rollout fails, mark it as `FAILED` in the history.
    - Allow `kbox rollback` even if the current state is a failed rollout.
- [ ] **Log Stream Robustness:** Switch from `bufio.Scanner` to a custom reader that handles lines > 64KB without crashing.
- [ ] **Code Generation Cleanup:** Fix the "Deadweight ConfigMap" bug where generated ConfigMaps are not used.

## Milestone 2: Production Readiness
*Focus: Security, Observability, and Operations.*

- [ ] **Dependency Readiness Gates:** Auto-generate `initContainers` or `nc -z` checks for app containers to wait for their databases to be "Ready".
- [ ] **Advanced Networking:**
    - Add `egress` configuration to `kbox.yaml` to allow external API access without disabling the whole NetworkPolicy.
    - Support for `LoadBalancer` and `NodePort` with auto-detected service ports.
- [ ] **Observability Defaults:**
    - Optional `metrics: true` to auto-generate Prometheus `ServiceMonitor` resources.
    - Standardized structured logging configuration (injected via ENV).
- [ ] **Secret Management:** Support for external secret providers (AWS Secret Manager, HashiCorp Vault) via standard operators.

## Milestone 3: Ecosystem & Multi-Service
*Focus: Scale and migration.*

- [ ] **Full Multi-Service Support:**
    - Enable `kbox.yaml` to define multiple `kind: App` in one file (or a `kbox-workspace.yaml`).
    - Unified `kbox deploy` that handles dependencies between services in the same repo.
- [ ] **Improved Importer:**
    - Handle `Helm` chart rendering as an input to `kbox import`.
    - Detect `Ingress` and `HorizontalPodAutoscaler` during import.
- [ ] **Custom Templates:** Allow teams to define their own "hardened" base templates for company-wide standards.

## Milestone 4: Developer Experience (v1.0 Candidate)
*Focus: The "Wow" factor.*

- [ ] **`kbox dev` Watch Mode Improvements:** 
    - Instant hot-reloading for local clusters without full image rebuilds (using file syncing like Skaffold/Tilt).
- [ ] **Interactive `doctor`:** Automatically offer to fix common issues (e.g., "Namespace missing, create it? [Y/n]").
- [ ] **Plugin System:** Allow third-party tools to hook into the `render` or `deploy` lifecycles.

---

### Priority 0 Tasks (Start here):
1. Fix YAML Comment Strip.
2. Fix ConfigMap/Secret usage in Deployment.
3. Fix Rollout Recovery status.
