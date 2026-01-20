# Bug Hunter Report #4

**Tester:** Agent 4
**Date:** 2026-01-20
**Testing Duration:** Approximately 45 minutes
**Environment:** Local Kind cluster (kind-kbox-test), macOS Darwin 25.2.0

## Executive Summary

kbox is a well-designed Kubernetes deployment CLI with generally good error handling and user experience. Testing revealed 5 bugs of varying severity, primarily related to validation gaps and command inconsistencies. The tool successfully handles most common use cases but has opportunities for improvement in edge case validation and API consistency.

## Bugs Found

### BUG-1: Invalid Dependency Type Not Validated Until Render
- **Severity:** Medium
- **Component:** `internal/config/validate.go`, `internal/render/dependency.go`
- **Steps to Reproduce:**
  1. Create a kbox.yaml with an invalid dependency type:
     ```yaml
     apiVersion: kbox.dev/v1
     kind: App
     metadata:
       name: test-app
     spec:
       image: test:v1
       port: 8080
       dependencies:
         - type: oracle
     ```
  2. Run `kbox validate -f invalid-dependency.yaml`
  3. Observe it passes validation
  4. Run `kbox render -f invalid-dependency.yaml`
- **Expected Behavior:** `kbox validate` should catch invalid dependency types (only postgres, redis, mongodb, mysql are supported)
- **Actual Behavior:** Validation passes, error only occurs during render:
```
failed to render: unsupported dependency type: oracle
  -> Supported: [postgres redis mongodb mysql]
```
- **Root Cause:** The `Validate()` function in `internal/config/validate.go` does not check dependency types. Validation happens in `RenderDependency()` at render time.

---

### BUG-2: `kbox down` Command Missing `-f` Flag (API Inconsistency)
- **Severity:** Low
- **Component:** `internal/cli/down.go`
- **Steps to Reproduce:**
  1. Run `kbox down -f test-configs/valid-minimal.yaml --force`
- **Expected Behavior:** Should accept `-f` flag like `deploy`, `validate`, and `render` commands
- **Actual Behavior:**
```
unknown shorthand flag: 'f' in -f
```
- **Impact:** Users cannot specify an alternate kbox.yaml path for the `down` command, creating an inconsistent API across commands. Must be in a directory with kbox.yaml to use `down`.

---

### BUG-3: Autoscaling minReplicas > maxReplicas Not Validated
- **Severity:** Medium
- **Component:** `internal/config/validate.go`
- **Steps to Reproduce:**
  1. Create a kbox.yaml with invalid autoscaling config:
     ```yaml
     apiVersion: kbox.dev/v1
     kind: App
     metadata:
       name: test-app
     spec:
       image: test:v1
       port: 8080
       autoscaling:
         enabled: true
         minReplicas: 10
         maxReplicas: 5
     ```
  2. Run `kbox validate -f autoscaling-invalid.yaml`
  3. Run `kbox render -f autoscaling-invalid.yaml`
- **Expected Behavior:** Validation should fail when minReplicas > maxReplicas
- **Actual Behavior:** Validation passes. Render generates invalid HPA with minReplicas: 10, maxReplicas: 5
```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
spec:
  maxReplicas: 5
  minReplicas: 10  # Invalid: min > max
```
- **Impact:** Invalid Kubernetes manifest generated; will fail when applied to cluster

---

### BUG-4: `kbox diff` Shows Misleading Image Change for Build-Only Configs
- **Severity:** Low
- **Component:** `internal/cli/diff.go`
- **Steps to Reproduce:**
  1. Create and deploy an app using `kbox template api test-api --lang go`
  2. Run `kbox up --no-logs` to build and deploy
  3. Run `kbox diff`
- **Expected Behavior:** Should show no changes or indicate build-based image
- **Actual Behavior:**
```
Changes for test-api (namespace: default)

   Service/test-api
 ~ Deployment/test-api (update)
     image: test-api:kbox-1768933182 ->
```
- **Impact:** Confusing output - shows image changing to empty string because kbox.yaml uses `build:` instead of `image:`

---

### BUG-5: JSON Output Mode Still Outputs "validation failed" to stderr
- **Severity:** Low
- **Component:** `internal/cli/validate.go`
- **Steps to Reproduce:**
  1. Run `kbox validate -f missing-name.yaml --output=json`
- **Expected Behavior:** JSON output only, clean machine-readable output
- **Actual Behavior:**
```json
{
  "valid": false,
  "strict": false,
  "errors": [
    "validation failed:\n  - metadata.name: required"
  ],
  "file": "test-configs/missing-name.yaml"
}
validation failed
```
- **Impact:** The "validation failed" text after the JSON breaks machine parsing

---

## Commands Tested

| Command | Status | Notes |
|---------|--------|-------|
| kbox doctor | Pass | Correctly identifies setup issues |
| kbox version | Pass | Shows version info |
| kbox init | Pass | Creates kbox.yaml, handles existing file |
| kbox validate | Partial | Missing dependency type and autoscaling validation |
| kbox render | Pass | Correct manifest generation |
| kbox deploy | Pass | Successful SSA deployment |
| kbox deploy --dry-run | Pass | Shows preview correctly |
| kbox diff | Partial | Misleading output for build configs |
| kbox up | Pass | Build + deploy works well |
| kbox down | Partial | Missing -f flag |
| kbox status | Pass | Rich status output |
| kbox logs | Pass | Streams logs correctly |
| kbox history | Pass | Shows release history |
| kbox rollback | Pass | Correct error when no previous release |
| kbox pf | Pass | Port-forward works |
| kbox shell | Pass | Error handling for missing pods |
| kbox add | Pass | Adds dependencies correctly |
| kbox remove | Pass | Removes dependencies, handles edge cases |
| kbox expose | Pass | Creates ingress |
| kbox unexpose | Pass | Deletes ingress |
| kbox dns | Pass | Shows DNS records |
| kbox graph | Pass | ASCII and Mermaid output work |
| kbox template | Pass | Generates project scaffolds |
| kbox env | Pass | Manages env vars in kbox.yaml |
| kbox import | Pass | Converts K8s YAML to kbox format |

## Edge Cases Tested

- [x] Invalid YAML syntax - Caught with helpful error
- [x] Missing required fields (metadata.name, spec.image) - Caught
- [x] Invalid port numbers (negative, >65535) - Caught
- [x] Invalid app names (special chars, too long) - Caught
- [x] Invalid resource quantities - Caught
- [x] Resource request exceeds limit - Caught
- [x] Negative replicas - Caught
- [x] Empty config file - Caught
- [x] Non-existent file - Caught
- [x] Duplicate dependency add - Caught
- [x] Remove non-existent dependency - Caught
- [ ] Invalid dependency type - NOT caught at validation
- [ ] Autoscaling min > max - NOT caught
- [x] Long app names (>63 chars) - Caught
- [x] Special characters in env values - Handled correctly
- [x] Missing cluster connection - Helpful error message
- [x] Image not found (ErrImagePull) - Good error reporting

## Security Observations

1. **Good Practices:**
   - Security contexts correctly applied (runAsNonRoot, readOnlyRootFilesystem, dropped capabilities)
   - ServiceAccounts with automountServiceAccountToken disabled
   - NetworkPolicies generated by default
   - Secrets properly base64 encoded
   - `--redact` flag available for hiding secrets in render output

2. **Minor Concerns:**
   - Password generation for dependencies uses a simple hex string (appears secure but implementation not audited)
   - Secrets displayed in plain render output by default (users should use `--redact`)

## Tool Rating

| Category | Score (1-10) | Notes |
|----------|--------------|-------|
| Stability | 9 | No crashes or panics observed |
| Error Handling | 7 | Good messages, some validation gaps |
| Documentation Accuracy | 8 | README matches behavior well |
| Edge Case Handling | 7 | Most handled, some validation missing |
| User Experience | 9 | Great CLI design, helpful hints |
| **Overall** | **8** | Solid tool with minor issues |

## Recommendations

1. **Priority 1 - Add validation for dependency types**
   - Move dependency type checking from render to validate
   - Location: `internal/config/validate.go`
   - Simple fix: Add loop to check `config.Spec.Dependencies[].Type` against `dependencies.SupportedTypes()`

2. **Priority 2 - Add autoscaling validation**
   - Validate minReplicas <= maxReplicas when autoscaling enabled
   - Location: `internal/config/validate.go`

3. **Priority 3 - Add `-f` flag to `kbox down` command**
   - Maintain API consistency across commands
   - Location: `internal/cli/down.go`
   - Pattern: Follow `deploy.go` implementation

## Additional Notes

- The tool handled a real build+deploy cycle flawlessly
- Port-forward, logs, and status commands work well with running apps
- The graph visualization (both ASCII and Mermaid) is a nice feature
- Template generation creates working, deployable applications
- CI mode and JSON output are well implemented (with minor stderr issue)
