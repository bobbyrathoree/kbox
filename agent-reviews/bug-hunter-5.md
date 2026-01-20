# Bug Hunter Report #5

**Tester:** Agent 5
**Date:** 2026-01-20
**Testing Duration:** ~45 minutes
**Environment:** Local (kind cluster), macOS (Darwin 25.2.0, arm64)

## Executive Summary

kbox is a well-designed tool with comprehensive features for simplifying Kubernetes deployments. The core commands work reliably, but I discovered several bugs ranging from critical security context issues that prevent common images from running, to less severe issues like silent failures with invalid environment names and broken JSON output formatting. The tool would benefit from better default handling for read-only filesystems and improved validation of user inputs.

## Bugs Found

### BUG-1: Default Security Context Breaks Common Images (nginx, etc.)
- **Severity:** High
- **Component:** `internal/render/deployment.go` - Security context defaults
- **Steps to Reproduce:**
  1. Create a simple kbox.yaml:
     ```yaml
     apiVersion: kbox.dev/v1
     kind: App
     metadata:
       name: testapp
     spec:
       image: nginx:1.25
       port: 80
       replicas: 1
     ```
  2. Run `kbox deploy`
  3. Pod enters CrashLoopBackOff
- **Expected Behavior:** nginx should start successfully or kbox should warn about read-only filesystem compatibility
- **Actual Behavior:** nginx crashes with:
  ```
  nginx: [emerg] mkdir() "/var/cache/nginx/client_temp" failed (30: Read-only file system)
  ```
- **Root Cause:** `readOnlyRootFilesystem: true` is applied by default without providing writable volumes for common image requirements
- **Workaround:** User must manually add emptyDir volumes:
  ```yaml
  volumes:
    - name: nginx-cache
      mountPath: /var/cache/nginx
      emptyDir: true
    - name: nginx-run
      mountPath: /var/run
      emptyDir: true
  ```
- **Recommendation:** Either detect common images and auto-add required volumes, or provide a `security.readOnlyRootFilesystem: false` option in the spec

---

### BUG-2: Non-existent Environment Name Silently Succeeds
- **Severity:** Medium
- **Component:** `internal/config/schema.go` - `ForEnvironment()` function
- **Steps to Reproduce:**
  1. Use a config with defined environments (dev, staging, prod)
  2. Run `kbox render -e nonexistent`
- **Expected Behavior:** Error or warning that "nonexistent" environment doesn't exist
- **Actual Behavior:** Command proceeds silently using base config, no warning
- **Error Output:** None (silent success)
```
Warning: image uses :latest tag - consider pinning to a specific version for reproducibility
Using environment: nonexistent

apiVersion: v1
automountServiceAccountToken: false
...
```
- **Risk:** Users may deploy with wrong configuration thinking they're using a specific environment
- **Recommendation:** Add validation in `ForEnvironment()` to warn or error when environment name doesn't exist in the config

---

### BUG-3: `kbox share` Uses Wrong Port (Ignores kbox.yaml)
- **Severity:** Medium
- **Component:** `kbox share` command
- **Steps to Reproduce:**
  1. Create kbox.yaml with `port: 80`
  2. Deploy the app
  3. Run `kbox share testapp`
- **Expected Behavior:** Should port-forward to port 80 as specified in kbox.yaml
- **Actual Behavior:** Port-forward shows `localhost:55582 -> testapp-...:8080` (uses default 8080)
- **Error Output:**
```
Port-forward established: localhost:55582 -> testapp-679789c9b-7l5ff:8080
```
- **Impact:** Share functionality fails to connect to the correct application port

---

### BUG-4: JSON Output Mixed with Plain Text on Validation Failure
- **Severity:** Medium
- **Component:** `kbox validate` command output handling
- **Steps to Reproduce:**
  1. Run `kbox validate --strict -f examples/full-app/kbox.yaml --output=json`
- **Expected Behavior:** Pure JSON output (even on error)
- **Actual Behavior:** JSON followed by plain text "validation failed"
- **Error Output:**
```json
{
  "valid": false,
  "strict": true,
  "warnings": [
    "image uses :latest tag - consider pinning to a specific version for reproducibility"
  ],
  "file": "examples/full-app/kbox.yaml"
}
validation failed
```
- **Impact:** Breaks CI/CD pipelines that parse JSON output

---

### BUG-5: Postgres Dependency Fails with Permission Issues
- **Severity:** Medium
- **Component:** `internal/render/dependency.go` - postgres security context
- **Steps to Reproduce:**
  1. Create a config with postgres dependency
  2. Deploy with `kbox up` or `kbox deploy`
  3. Observe postgres StatefulSet pod crash loop
- **Expected Behavior:** Postgres should initialize and run correctly
- **Actual Behavior:** Postgres fails with permission error:
```
chmod: /var/lib/postgresql/data: Operation not permitted
initdb: error: could not change permissions of directory "/var/lib/postgresql/data": Operation not permitted
```
- **Analysis:** The security context sets `runAsUser: 70` (postgres user), but the PVC may not be properly owned. The postgres alpine image expects certain permissions during initdb.
- **Recommendation:** Consider using an init container to set permissions, or document this limitation for certain storage backends

---

### BUG-6: `kbox render` Fails When env File is Missing (Path Resolution Issue)
- **Severity:** Low
- **Component:** `kbox render` command
- **Steps to Reproduce:**
  1. From the project root, run `kbox render -f examples/full-app/kbox.yaml`
  2. (Note: full-app/kbox.yaml references `secrets.fromEnvFile: .env`)
- **Expected Behavior:** Should resolve .env relative to the kbox.yaml file location
- **Actual Behavior:** Tries to load .env from current working directory
- **Error Output:**
```
failed to render: failed to open env file: open .env: no such file or directory
```
- **Workaround:** Run the command from the directory containing kbox.yaml

---

### BUG-7: `kbox doctor` Reports Failure for Optional sops
- **Severity:** Low
- **Component:** `kbox doctor` command
- **Steps to Reproduce:**
  1. Run `kbox doctor` without sops installed
- **Expected Behavior:** Should pass all required checks and note sops as optional
- **Actual Behavior:** Reports "Some checks failed" when only sops (marked as optional) is missing
- **Error Output:**
```
  ...
  ✗ sops: not found (optional, for encrypted secrets)
  ...
Some checks failed. Fix the issues above to use kbox effectively.
```
- **Recommendation:** Don't count optional tools as failures in the summary message

---

## Commands Tested

| Command | Status | Notes |
|---------|--------|-------|
| kbox init | Pass | Works well, auto-detects Dockerfile settings |
| kbox validate | Pass | Good error messages, strict mode works |
| kbox render | Pass | With path resolution caveat (BUG-6) |
| kbox deploy | Pass | Solid deployment with SSA |
| kbox up | Pass | Full build-deploy-logs workflow |
| kbox down | Pass | Proper cleanup, preserves PVCs by default |
| kbox status | Pass | Good status display with events |
| kbox logs | Pass | K8s events interleaved nicely |
| kbox pf | Pass | Simple port-forward syntax |
| kbox shell | Pass | Works with running pods |
| kbox exec | Pass | Creates temp pod correctly |
| kbox diff | Pass | Shows changes clearly |
| kbox history | Pass | Release tracking works |
| kbox rollback | Pass | Works when releases exist |
| kbox rollout status | Pass | Clear status display |
| kbox rollout canary | Pass | Canary deployment works |
| kbox rollout promote | Pass | Promotes canary correctly |
| kbox add | Pass | Modifies kbox.yaml correctly |
| kbox remove | Pass | Removes dependency correctly |
| kbox env | Pass | List/set/unset works |
| kbox expose | Pass | Creates ingress correctly |
| kbox unexpose | Pass | Removes ingress |
| kbox dns | Pass | Shows DNS records needed |
| kbox graph | Pass | ASCII and Mermaid output |
| kbox share | Issue | Wrong port (BUG-3) |
| kbox import | Pass | Imports k8s YAML correctly |
| kbox template | Pass | Generates scaffolds |
| kbox doctor | Issue | Optional check reporting (BUG-7) |
| kbox version | Pass | Displays version info |
| kbox events | Pass | Streams events correctly |
| kbox preview | Not Tested | Would need multi-namespace setup |
| kbox promote | Not Tested | Would need multi-env setup |
| kbox db | Not Tested | Postgres dependency failing |
| kbox job | Not Tested | Would need job definition |
| kbox dashboard | Not Tested | Interactive TUI |
| kbox profile | Not Tested | Profiling feature |
| kbox trace | Not Tested | Tracing feature |

## Edge Cases Tested

- [x] Invalid YAML syntax - Good error message
- [x] Missing required fields (name, image/build) - Good validation
- [x] Invalid Kubernetes name format - Good validation
- [x] Invalid port numbers (negative, >65535) - Good validation
- [x] Negative replicas - Good validation
- [x] Zero replicas - Allowed (valid use case)
- [x] Non-existent file path - Good error message
- [x] Non-existent namespace - Good error with suggestion
- [x] Non-existent environment - Silent success (BUG-2)
- [x] Duplicate dependency add - Good error message
- [x] Unsupported dependency type - Good error with suggestions
- [x] kbox.yaml already exists - Proper handling with --force
- [x] No kbox.yaml in directory - Good error message
- [x] Empty config file - Proper validation error
- [ ] Network timeouts - Not tested
- [ ] Permission denied scenarios - Not tested
- [x] Rollback with single release - Good error message

## Security Observations

1. **Secrets in JSON output**: The `kbox deploy --dry-run --output=json` includes secrets in plaintext. While useful for debugging, this could be a concern in CI logs.

2. **Password generation**: Passwords for dependencies appear to be randomly generated, which is good. The generation function should be reviewed to ensure it uses cryptographically secure random numbers.

3. **Default security context**: The default security context is quite restrictive (non-root, read-only fs, dropped capabilities, seccomp), which is excellent for security but causes usability issues (BUG-1).

4. **Network policies**: Auto-generated network policies are a good security feature, limiting pod-to-pod communication appropriately.

5. **ServiceAccount**: Disabling token automount by default is a good security practice.

## Tool Rating

| Category | Score (1-10) | Notes |
|----------|--------------|-------|
| Stability | 7 | Core commands stable, some edge cases fail silently |
| Error Handling | 8 | Most errors have clear messages with suggestions |
| Documentation Accuracy | 7 | README matches behavior, some undocumented edge cases |
| Edge Case Handling | 6 | Silent failures on invalid environment, security context issues |
| **Overall** | **7** | Solid tool with room for improvement on defaults |

## Recommendations

1. **Fix security context defaults for common images** (BUG-1): This is the most impactful issue. Either auto-detect common images that need writable directories (nginx, redis, etc.) and add appropriate emptyDir volumes, or provide a simple config option to disable read-only filesystem.

2. **Validate environment names** (BUG-2): Add a warning or error when using `--env` with a non-existent environment name. This prevents accidental deployments with wrong configurations.

3. **Fix JSON output formatting** (BUG-4): Ensure all output modes (text/json) are consistent. Error messages should be part of the JSON structure when `--output=json` is specified, not appended as plain text.

4. **Improve documentation for security context**: Document the security context defaults and their implications. Provide examples for common images that require additional configuration.

5. **Fix `kbox share` port detection** (BUG-3): The share command should read the port from kbox.yaml or the running deployment, not use a hardcoded default.
