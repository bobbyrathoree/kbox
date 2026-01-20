# Bug Hunter Report #2

**Tester:** Agent 2
**Date:** 2026-01-20
**Testing Duration:** Approximately 45 minutes
**Environment:** local (kind cluster named kbox-test, Kubernetes v1.35.0)

## Executive Summary

kbox is a well-designed CLI tool for simplifying Kubernetes deployments with sensible defaults and good error handling for common cases. However, testing revealed 9 bugs ranging from critical (hardcoded security settings causing container crashes) to medium (validation gaps for invalid configurations). The most impactful issues are related to the readOnlyRootFilesystem security setting and missing validation for several configuration options.

## Bugs Found

### BUG-1: readOnlyRootFilesystem Causes Common Containers to Crash
- **Severity:** Critical
- **Component:** `/internal/render/deployment.go` (lines 36-46)
- **Steps to Reproduce:**
  1. Create a simple kbox.yaml with nginx image:
     ```yaml
     apiVersion: kbox.dev/v1
     kind: App
     metadata:
       name: nginx-test
     spec:
       image: nginx:1.25
       port: 80
       replicas: 2
     ```
  2. Run `kbox deploy`
  3. Observe pods crash with CrashLoopBackOff
- **Expected Behavior:** nginx (and other common containers) should start successfully
- **Actual Behavior:** Containers crash because nginx needs to write to `/var/cache/nginx/` and `/etc/nginx/conf.d/`
- **Error Output:**
```
2026/01/20 18:14:12 [emerg] 1#1: mkdir() "/var/cache/nginx/client_temp" failed (30: Read-only file system)
nginx: [emerg] mkdir() "/var/cache/nginx/client_temp" failed (30: Read-only file system)
```
- **Root Cause:** `readOnlyRootFilesystem: true` is hardcoded in `defaultContainerSecurityContext()` with no way to disable it
- **Recommendation:** Add a config option like `spec.security.readOnlyRootFilesystem: false` to allow users to disable this for containers that need write access

---

### BUG-2: Non-existent Environment Name Silently Succeeds
- **Severity:** Medium
- **Component:** `/internal/config/schema.go` (ForEnvironment method)
- **Steps to Reproduce:**
  1. Create kbox.yaml with environments defined (dev, staging, prod)
  2. Run `kbox render -e nonexistent`
  3. Observe it renders without any warning
- **Expected Behavior:** Should warn or fail when referencing a non-existent environment
- **Actual Behavior:** Silently renders with base config, potentially deploying wrong configuration
- **Error Output:** None - just renders saying "Using environment: nonexistent"
- **Recommendation:** Add validation warning when specified environment doesn't exist in config

---

### BUG-3: Validator Accepts Invalid Autoscaling Config (minReplicas > maxReplicas)
- **Severity:** Medium
- **Component:** `/internal/config/validate.go`
- **Steps to Reproduce:**
  1. Create kbox.yaml:
     ```yaml
     apiVersion: kbox.dev/v1
     kind: App
     metadata:
       name: test
     spec:
       image: test:v1
       port: 8080
       autoscaling:
         enabled: true
         minReplicas: 5
         maxReplicas: 3
     ```
  2. Run `kbox validate -f <file>`
- **Expected Behavior:** Validation should fail with error about minReplicas exceeding maxReplicas
- **Actual Behavior:** Validation passes with "No warnings"
- **Error Output:** None - validation reports success
- **Recommendation:** Add validation rule to check minReplicas <= maxReplicas

---

### BUG-4: Resource Summary Undercounts When Jobs/CronJobs Are Present
- **Severity:** Low
- **Component:** `/internal/cli/render.go` (--summary logic)
- **Steps to Reproduce:**
  1. Create kbox.yaml with jobs:
     ```yaml
     spec:
       jobs:
         - name: migrate
           command: ['sh', '-c', 'echo done']
           runBefore: deploy
         - name: cleanup
           command: ['sh', '-c', 'echo cleanup']
           schedule: '0 * * * *'
     ```
  2. Run `kbox render --summary`
  3. Compare reported count with actual YAML output
- **Expected Behavior:** Summary should count all resources including Jobs and CronJobs
- **Actual Behavior:** Summary says "Total resources: 6" but full render shows 8 resources (Job and CronJob not counted)
- **Recommendation:** Update summary calculation to include batch/v1 Job and CronJob resources

---

### BUG-5: Postgres Dependency Crashes Due to Permission Issues
- **Severity:** High
- **Component:** `/internal/render/dependency.go`
- **Steps to Reproduce:**
  1. Create kbox.yaml with postgres dependency
  2. Run `kbox deploy`
  3. Check postgres pod status
- **Expected Behavior:** Postgres should start and initialize successfully
- **Actual Behavior:** Pod enters CrashLoopBackOff with permission errors
- **Error Output:**
```
chmod: /var/lib/postgresql/data: Operation not permitted
initdb: error: could not change permissions of directory "/var/lib/postgresql/data": Operation not permitted
```
- **Root Cause:** The security context runs as uid 70 (postgres) but the PVC root is owned by root, and InitDB fails to change permissions
- **Recommendation:** Consider using an initContainer with appropriate permissions or adjusting the security context fsGroup handling

---

### BUG-6: Init Container Without Command Passes Validation
- **Severity:** Medium
- **Component:** `/internal/config/validate.go`
- **Steps to Reproduce:**
  1. Create kbox.yaml:
     ```yaml
     spec:
       initContainers:
         - name: init
     ```
  2. Run `kbox validate`
- **Expected Behavior:** Validation should fail - command is required for init containers
- **Actual Behavior:** Validation passes, renders an init container with no command
- **Recommendation:** Add validation to require `command` field for init containers

---

### BUG-7: Volume Without Source Passes Validation and Renders Broken YAML
- **Severity:** Medium
- **Component:** `/internal/config/validate.go`, `/internal/render/volume.go`
- **Steps to Reproduce:**
  1. Create kbox.yaml:
     ```yaml
     spec:
       volumes:
         - name: test-volume
           mountPath: /data
     ```
  2. Run `kbox validate` - passes
  3. Run `kbox render` - renders incomplete volume
- **Expected Behavior:** Validation should require one of: size, emptyDir, configMap, or secret
- **Actual Behavior:** Renders a volume with just a name (no source), which is invalid Kubernetes YAML:
```yaml
volumes:
- name: test-volume
```
- **Recommendation:** Add validation to require at least one volume source

---

### BUG-8: PDB With Both minAvailable and maxUnavailable Passes Validation
- **Severity:** Medium
- **Component:** `/internal/config/validate.go`
- **Steps to Reproduce:**
  1. Create kbox.yaml:
     ```yaml
     spec:
       pdb:
         minAvailable: 2
         maxUnavailable: 1
     ```
  2. Run `kbox validate`
- **Expected Behavior:** Validation should fail - PDB can only have one of these
- **Actual Behavior:** Validation passes, renders invalid PDB
- **Recommendation:** Add validation to ensure only one of minAvailable/maxUnavailable is set

---

### BUG-9: Invalid Cron Schedule Passes Validation
- **Severity:** Low
- **Component:** `/internal/config/validate.go`
- **Steps to Reproduce:**
  1. Create kbox.yaml:
     ```yaml
     spec:
       jobs:
         - name: cleanup
           schedule: 'invalid-cron-format'
           command: ['echo', 'test']
     ```
  2. Run `kbox validate`
- **Expected Behavior:** Validation should fail with invalid cron format error
- **Actual Behavior:** Validation passes with "No warnings"
- **Recommendation:** Add cron expression validation for job schedules

---

## Commands Tested

| Command | Status | Notes |
|---------|--------|-------|
| kbox doctor | Pass | Works correctly, shows clear status |
| kbox version | Pass | Shows version info correctly |
| kbox init | Pass | Auto-detects Dockerfile, creates valid config |
| kbox validate | Partial | Works for basic cases, missing many edge cases (see bugs 3,6,7,8,9) |
| kbox render | Partial | Core functionality works, --summary undercounts, environment handling issue |
| kbox deploy | Partial | Works but security context causes many containers to crash |
| kbox diff | Pass | Shows clear diff output |
| kbox add | Pass | Correctly adds dependencies, detects duplicates |
| kbox remove | Pass | Removes dependencies with helpful hints |
| kbox down | Pass | Cleans up resources with confirmation |
| kbox logs | Pass | Handles missing pods gracefully |
| kbox shell | Pass | Handles missing pods gracefully |
| kbox pf | Pass | Handles missing pods gracefully |
| kbox status | Pass | Shows comprehensive status |
| kbox history | Pass | Shows releases or helpful message |
| kbox rollback | Pass | Proper error when no releases exist |
| kbox expose | Pass | Creates ingress correctly |
| kbox unexpose | Pass | Removes ingress |
| kbox dns | Pass | Shows DNS records to create |
| kbox import | Pass | Converts K8s YAML to kbox format |
| kbox preview create | Pass | Creates isolated namespace |
| kbox preview list | Pass | Lists active previews |
| kbox preview destroy | Pass | Cleans up preview |
| kbox exec | Pass | Runs one-off commands in fresh pods |
| kbox graph | Pass | ASCII and Mermaid output work |
| kbox template | Pass | Generates valid project scaffolds |
| kbox env set/unset/list | Pass | Manages env vars in kbox.yaml |
| kbox events | Pass | Streams/shows events correctly |
| kbox rollout status | Pass | Handles missing deployment |
| kbox db connect | N/A | Tested but database wasn't running |
| kbox dashboard | N/A | Requires TTY, expected to fail in test |
| kbox share | N/A | Requires ngrok, not tested |

## Edge Cases Tested

- [x] Invalid YAML syntax - Caught with helpful error
- [x] Missing required fields (image/build) - Caught
- [x] Invalid port numbers (negative, >65535) - Caught
- [x] Invalid Kubernetes names (uppercase, too long) - Caught
- [x] Non-existent namespaces - Caught with helpful message
- [x] Non-existent config files - Caught
- [x] Empty/missing app name - Caught
- [x] Negative replicas - Caught
- [x] Invalid resource formats - Caught
- [x] Ingress without host - Caught
- [ ] minReplicas > maxReplicas - NOT caught (BUG-3)
- [ ] Non-existent environment - NOT caught (BUG-2)
- [ ] Init container without command - NOT caught (BUG-6)
- [ ] Volume without source - NOT caught (BUG-7)
- [ ] PDB with both min/max - NOT caught (BUG-8)
- [ ] Invalid cron schedule - NOT caught (BUG-9)

## Security Observations

1. **readOnlyRootFilesystem**: While secure by default, the inability to disable it breaks many common containers. This is a usability vs security tradeoff that should be configurable.

2. **Service Account Token Automount**: Correctly disabled by default (`automountServiceAccountToken: false`)

3. **Security Context**: Good defaults applied:
   - runAsNonRoot: true
   - allowPrivilegeEscalation: false
   - Capabilities dropped: ALL
   - SeccompProfile: RuntimeDefault

4. **Secrets in Output**: `--redact` flag works correctly for hiding secrets in render output

5. **Database Passwords**: Auto-generated with sufficient entropy (32 hex characters)

## Tool Rating

| Category | Score (1-10) | Notes |
|----------|--------------|-------|
| Stability | 7 | No crashes or panics, but security settings cause apps to fail |
| Error Handling | 8 | Good error messages, helpful hints for fixes |
| Documentation Accuracy | 8 | README matches behavior for most cases |
| Edge Case Handling | 5 | Many validation gaps for invalid configs |
| **Overall** | **7** | Solid foundation with good UX, needs validation improvements |

## Recommendations

### Top 3 Fixes:

1. **Add config option to disable readOnlyRootFilesystem** (BUG-1)
   - This is blocking basic nginx deployments
   - Add `spec.security.readOnlyRootFilesystem` option
   - Consider detecting common images that need write access

2. **Expand validation coverage** (BUGs 3, 6, 7, 8, 9)
   - Add autoscaling minReplicas <= maxReplicas check
   - Require command for init containers
   - Require volume source (size, emptyDir, configMap, or secret)
   - Enforce mutual exclusivity of PDB minAvailable/maxUnavailable
   - Validate cron expressions

3. **Fix postgres dependency permissions** (BUG-5)
   - Either use an initContainer to fix permissions
   - Or adjust fsGroup/runAsUser/runAsGroup configuration
   - Consider documenting this as a known limitation

### Additional Improvements:
- Warn when specified environment doesn't exist (BUG-2)
- Fix --summary resource counting to include Jobs/CronJobs (BUG-4)
