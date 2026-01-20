# Bug Hunter Report #3

**Tester:** Agent 3
**Date:** 2026-01-20
**Testing Duration:** ~45 minutes
**Environment:** Local kind cluster (kindest/node:v1.35.0)

## Executive Summary

I conducted extensive testing of `kbox` across all major command categories. The tool is generally well-designed with good error handling for common cases, but I found 6 bugs ranging from validation gaps to misleading exit codes. The most significant issues involve validation not catching invalid configurations that would fail at deployment time.

## Bugs Found

### BUG-1: kbox init creates invalid kbox.yaml with spaces in name
- **Severity:** Medium
- **Component:** `kbox init`
- **Steps to Reproduce:**
  1. `mkdir /tmp/init-test && cd /tmp/init-test`
  2. `touch Dockerfile`
  3. `kbox init --force --name "my app"`
- **Expected Behavior:** Should fail with error about invalid name containing spaces
- **Actual Behavior:** Creates kbox.yaml with `name: my app` which then fails `kbox validate`
- **Error Output:**
```
# kbox init succeeds:
Created kbox.yaml
Configuration:
  name:     my app

# But validate fails:
Invalid configuration: kbox.yaml
  Error: validation failed:
  - metadata.name: must be lowercase alphanumeric with hyphens, max 63 chars
```

### BUG-2: Non-existent environment overlay silently succeeds
- **Severity:** Medium
- **Component:** `kbox render`, `kbox deploy`
- **Steps to Reproduce:**
  1. `kbox render -e nonexistent -f examples/payments/kbox.yaml`
- **Expected Behavior:** Should fail with error "environment 'nonexistent' not found in kbox.yaml"
- **Actual Behavior:** Silently uses base config, prints "Using environment: nonexistent"
- **Error Output:**
```
Using environment: nonexistent
apiVersion: v1
... (proceeds with base config)
Exit code: 0
```
- **Impact:** Users may unknowingly deploy wrong configuration to production

### BUG-3: Validation accepts minReplicas > maxReplicas in autoscaling
- **Severity:** Medium
- **Component:** `kbox validate`
- **Steps to Reproduce:**
  1. Create kbox.yaml with:
     ```yaml
     spec:
       autoscaling:
         enabled: true
         minReplicas: 5
         maxReplicas: 3
     ```
  2. Run `kbox validate`
- **Expected Behavior:** Should fail validation with "minReplicas cannot exceed maxReplicas"
- **Actual Behavior:** Passes validation, creates invalid HPA
- **Error Output:**
```
Valid configuration: kbox.yaml
  No warnings
```
- **Rendered output shows:** `HPA: edge-test (min: 5, max: 3)` - which Kubernetes will reject

### BUG-4: Duplicate dependencies not detected in validation
- **Severity:** Medium
- **Component:** `kbox validate`
- **Steps to Reproduce:**
  1. Create kbox.yaml with:
     ```yaml
     spec:
       dependencies:
         - type: postgres
         - type: postgres
     ```
  2. Run `kbox validate`
- **Expected Behavior:** Should fail with "duplicate dependency: postgres"
- **Actual Behavior:** Passes validation
- **Error Output:**
```
Valid configuration: kbox.yaml
  No warnings
```
- **Render output creates duplicate resources:**
```
Services:        3
  - edge-test-postgres
  - edge-test-postgres
  - edge-test
```

### BUG-5: kbox doctor exits with code 0 even when checks fail
- **Severity:** Low
- **Component:** `kbox doctor`
- **Steps to Reproduce:**
  1. `kbox doctor --context=nonexistent`
- **Expected Behavior:** Exit code 1 when critical checks fail
- **Actual Behavior:** Exit code 0 despite failures
- **Error Output:**
```
Results:
  ...
  X cluster connection: failed to build rest config: context "nonexistent" does not exist

Some checks failed. Fix the issues above to use kbox effectively.
Exit code: 0
```
- **Impact:** CI pipelines using `kbox doctor` won't fail on setup issues

### BUG-6: PDB validation allows both minAvailable and maxUnavailable
- **Severity:** Low
- **Component:** `kbox validate`
- **Steps to Reproduce:**
  1. Create kbox.yaml with:
     ```yaml
     spec:
       pdb:
         minAvailable: "50%"
         maxUnavailable: "50%"
     ```
  2. Run `kbox validate`
- **Expected Behavior:** Should warn or error since Kubernetes PDB allows only one of these
- **Actual Behavior:** Passes validation, renders invalid PDB
- **Error Output:**
```
Valid configuration: kbox.yaml
  No warnings
```
- **Note:** Kubernetes API will reject this, but kbox should catch it earlier

## Commands Tested

| Command | Status | Notes |
|---------|--------|-------|
| kbox up | OK | Works well with Dockerfile auto-detection |
| kbox deploy | OK | Proper rollout tracking, good error messages |
| kbox down | OK | Clean resource deletion |
| kbox status | OK | Excellent output with events |
| kbox logs | OK | Events interleaved nicely |
| kbox rollback | OK | Works correctly, saves as new release |
| kbox validate | BUG | Missing several validation checks |
| kbox render | OK | Clean YAML output, --redact works |
| kbox init | BUG | Allows invalid names |
| kbox add | OK | Modifies kbox.yaml correctly |
| kbox remove | OK | Clean removal |
| kbox diff | OK | Shows changes clearly |
| kbox expose | OK | Creates ingress correctly |
| kbox unexpose | OK | Removes ingress |
| kbox dns | OK | Shows pending LoadBalancer correctly |
| kbox graph | OK | ASCII and Mermaid work well |
| kbox history | OK | Shows release history |
| kbox rollout status | OK | Clear status display |
| kbox rollout canary | OK | Creates canary deployment |
| kbox rollout promote | OK | Promotes canary |
| kbox preview create | OK | Creates isolated namespace |
| kbox preview list | OK | Lists previews |
| kbox preview destroy | OK | Cleans up |
| kbox template | OK | Generates good scaffolds |
| kbox import | OK | Converts K8s manifests well |
| kbox env set/unset | OK | Modifies kbox.yaml correctly |
| kbox exec | OK | Works with --image override |
| kbox shell | OK | Ephemeral container fallback works |
| kbox doctor | BUG | Wrong exit code on failure |
| kbox events | OK | Good filtering, colorized output |
| kbox version | OK | Shows version info |
| kbox job | OK | Help available, not tested with actual jobs |
| kbox db | OK | Help available, not tested without database |

## Edge Cases Tested

- [x] Invalid YAML syntax - Proper error
- [x] Missing required fields - Proper error
- [x] Invalid port numbers (99999, -1) - Proper error
- [x] Negative replicas - Proper error
- [x] Very long app name (>63 chars) - Proper error
- [x] Uppercase in app name - Proper error
- [x] Non-existent namespace - Proper error with suggestion
- [x] Non-existent file - Proper error
- [x] Non-existent revision for rollback - Proper error
- [x] Invalid memory format - Proper error
- [x] Empty kbox.yaml - Proper error
- [x] MinReplicas > MaxReplicas - **NOT CAUGHT**
- [x] Duplicate dependencies - **NOT CAUGHT**
- [x] Non-existent environment - **SILENTLY IGNORED**
- [x] Invalid name via init - **NOT CAUGHT**
- [x] Both PDB settings - **NOT CAUGHT**

## Security Observations

1. **Good:** readOnlyRootFilesystem is enforced by default
2. **Good:** Non-root user (UID 1000) is enforced
3. **Good:** All capabilities dropped by default
4. **Good:** NetworkPolicies auto-generated
5. **Good:** ServiceAccount token automount disabled
6. **Good:** --redact flag properly hides secret values
7. **Note:** The enforced security settings (readOnlyRootFilesystem) may break some common images like nginx. Consider documenting this or providing an escape hatch.

## Tool Rating

| Category | Score (1-10) | Notes |
|----------|--------------|-------|
| Stability | 8 | No crashes or panics encountered |
| Error Handling | 7 | Good for most cases, but validation gaps exist |
| Documentation Accuracy | 9 | Help text matches behavior well |
| Edge Case Handling | 6 | Several validation gaps found |
| **Overall** | **7.5** | Solid tool with room for improvement in validation |

## Recommendations

1. **Fix validation gaps (Priority 1):** Add validation for:
   - minReplicas vs maxReplicas
   - Duplicate dependencies
   - Invalid names at init time
   - PDB conflicting settings

2. **Error on non-existent environment (Priority 2):** When `-e` specifies an environment not in kbox.yaml, fail with a clear error listing available environments.

3. **Fix doctor exit code (Priority 3):** Return exit code 1 when any required checks fail (sops is optional, so its absence shouldn't cause failure).

### Bonus Suggestions

- Consider adding a `--strict` mode that warns about potentially problematic configs (like images without tags, missing health checks)
- Add a `kbox lint` command for more comprehensive config checking
- Document which container images work well with the default security context
