# Bug Hunter Report #1

**Tester:** Agent 1
**Date:** 2026-01-20
**Testing Duration:** ~45 minutes
**Environment:** Local kind cluster (kind-kbox-test), macOS Darwin 25.2.0

## Executive Summary

kbox is a well-structured CLI tool with good error handling for most common scenarios. However, I discovered several significant bugs: the hardcoded read-only filesystem security setting breaks many standard Docker images (nginx, etc.), JSON output mode includes non-JSON text after valid JSON, and non-existent environment names are silently accepted without warning. Overall, the tool shows good promise but needs some fixes before production use.

## Bugs Found

### BUG-1: Hardcoded readOnlyRootFilesystem Breaks Standard Images
- **Severity:** Critical
- **Component:** `internal/render/deployment.go` - `defaultContainerSecurityContext()`
- **Steps to Reproduce:**
  1. Create a minimal kbox.yaml with `image: nginx:1.25`
  2. Run `kbox deploy`
  3. Check pod status with `kubectl get pods`
- **Expected Behavior:** nginx should start successfully
- **Actual Behavior:** Pod crashes in CrashLoopBackOff with error:
```
nginx: [emerg] mkdir() "/var/cache/nginx/client_temp" failed (30: Read-only file system)
```
- **Error Output:**
```
2026/01/20 18:15:17 [emerg] 1#1: mkdir() "/var/cache/nginx/client_temp" failed (30: Read-only file system)
```
- **Root Cause:** `readOnlyRootFilesystem: true` is hardcoded in `defaultContainerSecurityContext()` with no way to disable or configure writable paths
- **Impact:** Makes kbox unusable with many standard Docker images (nginx, redis without special config, etc.)
- **Suggested Fix:** Either make readOnlyRootFilesystem configurable in kbox.yaml, or auto-detect common images and add appropriate emptyDir volumes

---

### BUG-2: JSON Output Includes Non-JSON Text
- **Severity:** High
- **Component:** Validate command (likely `cmd/kbox/validate.go`)
- **Steps to Reproduce:**
  1. Create an invalid kbox.yaml (e.g., missing required fields)
  2. Run `kbox validate -f invalid.yaml -o json`
- **Expected Behavior:** Pure JSON output suitable for parsing
- **Actual Behavior:** JSON followed by "validation failed" text
- **Error Output:**
```json
{
  "valid": false,
  "strict": false,
  "errors": [
    "validation failed:\n  - metadata.name: required"
  ],
  "file": "test-workspace/missing-name.yaml"
}
validation failed
```
- **Impact:** Breaks CI/CD pipelines that parse JSON output; cannot use `jq` or other JSON parsers reliably
- **Suggested Fix:** When `--output=json` is set, all output (including error messages) should be part of the JSON structure, not printed separately

---

### BUG-3: Non-Existent Environment Names Silently Accepted
- **Severity:** Medium
- **Component:** Config environment handling (`internal/config/schema.go` - `ForEnvironment()`)
- **Steps to Reproduce:**
  1. Create kbox.yaml with environments: `dev`, `staging`, `prod`
  2. Run `kbox render -e typo-environment`
- **Expected Behavior:** Error or warning that environment "typo-environment" doesn't exist
- **Actual Behavior:** Says "Using environment: typo-environment" and renders with base config (no overlay applied)
- **Error Output:**
```
Warning: image uses :latest tag - consider pinning to a specific version for reproducibility
Using environment: nonexistent

[renders YAML without any environment overlay]
```
- **Impact:** Silent misconfiguration - users may deploy to production thinking they're using prod settings when they made a typo
- **Suggested Fix:** Validate that the specified environment exists in the config before proceeding, or at minimum show a warning

---

### BUG-4: db connect Attempts to Connect to Non-Existent Pods
- **Severity:** Low
- **Component:** `db connect` command
- **Steps to Reproduce:**
  1. Run `kbox db connect postgres` without any database deployed
- **Expected Behavior:** Clear error message that no database pod exists
- **Actual Behavior:** Finds a non-existent pod name and tries to connect to it
- **Error Output:**
```
Warning: database pod simple-app-postgres-0 is not ready
Connecting to simple-app-postgres...
Pod: simple-app-postgres-0

No shell available in container, using ephemeral debug container...
Created ephemeral container "kbox-debug-21826", waiting for it to start...
pods "simple-app-postgres-0" not found
```
- **Impact:** Confusing error messages; users might think something is partially configured when it isn't

---

### BUG-5: No Validation for Absurdly Large Resource Values
- **Severity:** Low
- **Component:** Resource validation
- **Steps to Reproduce:**
  1. Create kbox.yaml with `resources.memory: 999999999Ti`
  2. Run `kbox validate -f config.yaml`
- **Expected Behavior:** Warning about unrealistic resource values
- **Actual Behavior:** Passes validation without any warning
- **Error Output:**
```
Valid configuration: test-workspace/huge-resources.yaml
  No warnings
```
- **Impact:** Users could accidentally deploy with impossible resource requests, causing immediate scheduling failures

---

## Commands Tested

| Command | Status | Notes |
|---------|--------|-------|
| kbox init | Pass | Auto-detects Dockerfile correctly |
| kbox validate | Partial | JSON output bug, no resource sanity checks |
| kbox render | Pass | YAML output is valid, summary works |
| kbox deploy | Partial | Works but security defaults break common images |
| kbox diff | Pass | Shows proper diff output |
| kbox up | Not Tested | Requires Dockerfile build |
| kbox dev | Not Tested | Requires Dockerfile |
| kbox down | Pass | Cleans up all resources |
| kbox logs | Pass | Proper error for missing pods |
| kbox shell | Pass | Connects to running pods |
| kbox pf | Pass | Port forwarding works |
| kbox status | Pass | Shows comprehensive status |
| kbox history | Pass | Shows release history |
| kbox rollback | Pass | Rollback works correctly |
| kbox expose | Pass | Proper error when host missing |
| kbox unexpose | Pass | Handles missing ingress |
| kbox add | Pass | Adds dependencies correctly |
| kbox remove | Pass | Removes dependencies |
| kbox import | Pass | Imports K8s manifests |
| kbox env | Pass | Manages env vars |
| kbox job | Pass | Proper error for missing jobs |
| kbox db | Partial | connect has pod detection bug |
| kbox exec | Pass | Runs commands in fresh pods |
| kbox graph | Pass | Shows topology correctly |
| kbox template | Pass | Generates scaffolds |
| kbox doctor | Pass | Checks setup properly |
| kbox version | Pass | Shows version info |
| kbox rollout | Pass | Proper errors when no deployment |
| kbox preview | Not Tested | Requires full setup |
| kbox promote | Not Tested | Requires multi-cluster |
| kbox share | Not Tested | Requires ngrok |
| kbox dashboard | Not Tested | Interactive TUI |

## Edge Cases Tested

- [x] Invalid YAML syntax - properly detected
- [x] Missing required fields - properly validated
- [x] Invalid port numbers (negative, >65535) - properly rejected
- [x] Invalid app names (spaces, too long) - properly rejected
- [x] Non-existent namespaces - proper error message
- [ ] Permission denied scenarios - not tested
- [ ] Network timeouts - not tested
- [x] Non-existent files - proper error message
- [x] Empty files - proper error message
- [x] Zero replicas - accepted (valid)
- [x] Negative replicas - properly rejected
- [x] Special characters in env vars - handled correctly
- [x] Very large resource values - NO WARNING (bug)
- [x] Invalid resource units - properly rejected
- [x] Non-existent environments - NO WARNING (bug)

## Security Observations

1. **Good:** Security contexts are applied by default (non-root, dropped capabilities, seccomp)
2. **Good:** ServiceAccount auto-mount is disabled
3. **Good:** NetworkPolicies are auto-generated
4. **Issue:** readOnlyRootFilesystem is too aggressive and not configurable
5. **Good:** Secrets are not shown in `render --redact` output
6. **Note:** Uses Server-Side Apply (good for security/audit trail)

## Tool Rating

| Category | Score (1-10) | Notes |
|----------|--------------|-------|
| Stability | 7 | No crashes, but security defaults break common images |
| Error Handling | 8 | Good error messages, except for JSON output and silent env failures |
| Documentation Accuracy | 9 | Help text matches behavior |
| Edge Case Handling | 6 | Missing validation for unrealistic values, silent failures |
| **Overall** | **7** | Good foundation but needs critical fixes |

## Recommendations

### Top 3 Things to Fix First:

1. **Critical:** Make `readOnlyRootFilesystem` configurable or auto-add emptyDir volumes for known images. This is blocking basic usage with common images like nginx.

2. **High:** Fix JSON output to be pure JSON. Add a wrapper that captures all output when `--output=json` is specified and ensures only valid JSON is written to stdout.

3. **Medium:** Validate environment names exist before using them. At minimum, show a warning. Ideally, fail with an error unless `--allow-undefined-env` is passed.

### Additional Recommendations:

- Add sanity checks for resource values (warn if memory > 1Ti, CPU > 128 cores)
- Consider adding a `--security-relaxed` flag that disables readOnlyRootFilesystem
- Add a `kbox check` command that validates the rendered manifests against the cluster's admission policies before deploying
- Consider adding a `--strict` flag to deploy that fails on warnings
