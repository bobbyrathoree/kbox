# Investigation: readOnlyRootFilesystem Bug

## Summary

The hardcoded `readOnlyRootFilesystem: true` security setting in kbox breaks many common Docker images (nginx, Apache, PHP-FPM, etc.) that require write access to temporary directories. This is a critical usability bug reported by 4 out of 5 bug hunter agents, preventing users from deploying standard container images without manual workarounds.

## Code Location

The `readOnlyRootFilesystem` setting is hardcoded in three locations:

### 1. Main Container Security Context
**File:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/render/deployment.go`
**Lines:** 34-47

```go
// defaultContainerSecurityContext returns a secure container-level security context
// that prevents privilege escalation and drops all capabilities
func defaultContainerSecurityContext() *corev1.SecurityContext {
    allowPrivilegeEscalation := false
    readOnlyRootFilesystem := true  // <-- HARDCODED

    return &corev1.SecurityContext{
        AllowPrivilegeEscalation: &allowPrivilegeEscalation,
        ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
        Capabilities: &corev1.Capabilities{
            Drop: []corev1.Capability{"ALL"},
        },
    }
}
```

This function is called in:
- Line 115: Main container security context
- Line 303: Init container security context

### 2. Tracing Sidecar Security Context
**File:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/render/tracing.go`
**Lines:** 24-36

```go
// sidecarSecurityContext returns a hardened SecurityContext for sidecar containers
func sidecarSecurityContext() *corev1.SecurityContext {
    allowPrivilegeEscalation := false
    readOnlyRootFilesystem := true  // <-- HARDCODED

    return &corev1.SecurityContext{
        AllowPrivilegeEscalation: &allowPrivilegeEscalation,
        ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
        Capabilities: &corev1.Capabilities{
            Drop: []corev1.Capability{"ALL"},
        },
    }
}
```

### 3. Dependency Security Context (Handled Correctly)
**File:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/render/dependency.go`
**Lines:** 302-318

```go
// dependencySecurityContext returns security context appropriate for dependencies
// Databases need writable filesystem, so readOnlyRootFilesystem is disabled for them
func dependencySecurityContext(depType string) *corev1.SecurityContext {
    allowPrivilegeEscalation := false
    // Databases need writable filesystem
    readOnly := true
    if depType == "postgres" || depType == "mysql" || depType == "mongodb" || depType == "redis" {
        readOnly = false  // <-- EXCEPTION FOR KNOWN DEPENDENCIES
    }
    return &corev1.SecurityContext{
        AllowPrivilegeEscalation: &allowPrivilegeEscalation,
        ReadOnlyRootFilesystem:   &readOnly,
        ...
    }
}
```

**Note:** The dependency rendering code already has a mechanism to disable `readOnlyRootFilesystem` for known database images. This pattern could be extended to user applications.

## Root Cause Analysis

### Why Was It Designed This Way?

1. **Security-First Philosophy**: kbox explicitly prioritizes security by default. The README states:
   > "you get **secure, production-ready defaults** out of the box: Non-root containers with read-only filesystem"

2. **Pod Security Standards Compliance**: The code comments indicate intent to pass Pod Security Standards (PSS). While `readOnlyRootFilesystem` is NOT required by PSS (neither Baseline nor Restricted profiles), it is considered a security best practice.

3. **Defense in Depth**: A read-only root filesystem prevents:
   - Malicious code from writing executables to the container
   - Accidental modifications to container configuration
   - Persistence mechanisms for attackers

4. **Compliance Requirements**: Some organizations require read-only filesystems for PCI-DSS, SOC2, or internal security policies.

### The Design Oversight

The developers correctly identified that databases need writable filesystems (see `dependency.go`), but did not provide the same flexibility for user applications. This creates an inconsistency:
- Managed dependencies: configurable
- User containers: hardcoded

## Impact Analysis

### Images Known to Break

| Image | Broken Directories | Error Message |
|-------|-------------------|---------------|
| nginx | `/var/cache/nginx`, `/var/run`, `/tmp` | `mkdir() "/var/cache/nginx/client_temp" failed (30: Read-only file system)` |
| apache/httpd | `/var/run/apache2`, `/var/lock/apache2` | Permission denied |
| php-fpm | `/var/run`, `/tmp` | Cannot create socket |
| redis (standalone) | `/data` (when no volume configured) | Cannot dump RDB |
| haproxy | `/var/run` | Cannot create pid file |
| traefik | `/tmp` | Cannot write temp files |
| jenkins | `/var/jenkins_home` | Cannot initialize |
| grafana | `/var/lib/grafana` | Cannot write database |
| prometheus | `/prometheus` | Cannot write TSDB |

### Workaround Exists But Is Tedious

Users CAN work around this by adding emptyDir volumes:

```yaml
spec:
  volumes:
    - name: nginx-cache
      mountPath: /var/cache/nginx
      emptyDir: true
    - name: nginx-run
      mountPath: /var/run
      emptyDir: true
    - name: tmp
      mountPath: /tmp
      emptyDir: true
```

**Problems with this workaround:**
1. Requires knowledge of image internals
2. Different for every image
3. Defeats "simple by default" philosophy
4. Not documented

## Industry Research

### How Helm Charts Handle This

**Bitnami nginx Helm Chart:**
- Default: `readOnlyRootFilesystem: true`
- Provides `extraVolumes` and `extraVolumeMounts` for customization
- Template automatically mounts emptyDir volumes at:
  - `/tmp` (tmp-dir)
  - `/opt/bitnami/nginx/conf` (app-conf-dir)
  - `/opt/bitnami/nginx/logs` (app-logs-dir)
  - `/opt/bitnami/nginx/tmp` (app-tmp-dir)
- Uses init container to preserve log symlinks

**Key Insight:** Bitnami keeps `readOnlyRootFilesystem: true` but automatically adds ALL required emptyDir mounts. This is the industry best practice.

### Kubernetes Pod Security Standards

From official Kubernetes documentation:
- **Baseline Profile**: Does NOT require `readOnlyRootFilesystem`
- **Restricted Profile**: Does NOT require `readOnlyRootFilesystem`
- **Recommendation**: It's a best practice but not mandatory

### Other Tools

| Tool | Default | Configurable | Auto-mounts |
|------|---------|--------------|-------------|
| Bitnami Charts | true | Yes | Yes (per image) |
| Kustomize | No default | N/A | N/A |
| Tanka | No default | N/A | N/A |
| cdk8s | No default | N/A | N/A |
| Pulumi | No default | N/A | N/A |

## Proposed Fixes

### Option A: Make It Configurable

**Implementation:**
1. Add new config field in schema.go:
```go
type SecurityConfig struct {
    ReadOnlyRootFilesystem *bool `yaml:"readOnlyRootFilesystem,omitempty"`
    RunAsNonRoot           *bool `yaml:"runAsNonRoot,omitempty"`
    RunAsUser              *int64 `yaml:"runAsUser,omitempty"`
}
```

2. Add to AppSpec:
```go
type AppSpec struct {
    // ... existing fields
    Security *SecurityConfig `yaml:"security,omitempty"`
}
```

3. Usage:
```yaml
spec:
  image: nginx:1.25
  security:
    readOnlyRootFilesystem: false
```

**Pros:**
- Simple implementation
- Gives users full control
- Maintains secure default
- Explicit opt-out makes security decisions visible

**Cons:**
- Users must know when to disable
- Each new security setting needs config option
- Doesn't help users who don't know what's breaking

**Security Impact:**
- Low: Users explicitly choose to reduce security
- Auditable: Configuration makes security posture visible

### Option B: Auto-Detect Known Images and Add EmptyDir Mounts

**Implementation:**
1. Create image registry with known writable paths:
```go
var imageWritablePaths = map[string][]string{
    "nginx":    {"/var/cache/nginx", "/var/run", "/tmp"},
    "httpd":    {"/var/run/apache2", "/var/lock/apache2", "/tmp"},
    "php":      {"/var/run", "/tmp"},
    "haproxy":  {"/var/run", "/tmp"},
    "traefik":  {"/tmp"},
}
```

2. Detect image prefix and auto-add emptyDir volumes

**Pros:**
- Zero-config "just works" experience
- Maintains security (read-only + targeted emptyDirs)
- Follows Bitnami pattern

**Cons:**
- Maintenance burden (new images need updates)
- May not cover all image variants
- Implicit behavior could surprise users
- Image detection by prefix is fragile

**Security Impact:**
- Low: EmptyDir volumes are ephemeral and don't reduce security posture
- Actually maintains strong security while improving usability

### Option C: Default to False with Opt-In Hardening

**Implementation:**
1. Change default in `defaultContainerSecurityContext()`:
```go
readOnlyRootFilesystem := false
```

2. Add config to enable:
```yaml
spec:
  security:
    readOnlyRootFilesystem: true
```

**Pros:**
- Immediate fix for all affected images
- Simple change
- Compatible with "batteries included" philosophy

**Cons:**
- **Breaks kbox's security-first promise**
- Existing users may have security policies relying on current behavior
- Makes kbox less secure than competitors
- Goes against documented behavior

**Security Impact:**
- High: Significantly reduces default security posture
- Containers can be modified at runtime
- NOT RECOMMENDED

### Option D: Combination - Configurable + Smart Defaults

**Implementation:**
1. Add `security.readOnlyRootFilesystem` config option (Option A)
2. Add `security.writablePaths` for auto-mounting emptyDirs:
```yaml
spec:
  security:
    readOnlyRootFilesystem: true  # default
    writablePaths:
      - /var/cache/nginx
      - /var/run
      - /tmp
```

3. Optionally detect common images and suggest `writablePaths`

**Pros:**
- Maintains security by default
- Provides escape hatch when needed
- Explicit about what paths are writable
- No magic/implicit behavior

**Cons:**
- More complex implementation
- Users still need to know what paths are needed

**Security Impact:**
- Low: Explicit configuration, maintains read-only default
- Best balance of security and usability

## Recommended Fix

**Recommended: Option D (Combination Approach)**

**Phase 1 (Immediate Fix):**
1. Add `spec.security.readOnlyRootFilesystem` config option
2. Default to `true` (maintains current secure behavior)
3. Users can set to `false` as escape hatch
4. Update documentation with examples for common images

**Phase 2 (Enhancement):**
1. Add `spec.security.writablePaths: []string` option
2. Auto-generate emptyDir volumes for specified paths
3. Keeps read-only root but provides targeted write access
4. This is the pattern used by production-grade Helm charts

**Implementation Priority:**
- Phase 1 unblocks users immediately
- Phase 2 provides the "right" solution

**Example Configuration After Fix:**

```yaml
# Option 1: Disable entirely (quick fix)
spec:
  image: nginx:1.25
  security:
    readOnlyRootFilesystem: false

# Option 2: Keep secure but add writable paths (recommended)
spec:
  image: nginx:1.25
  security:
    readOnlyRootFilesystem: true  # default, can omit
    writablePaths:
      - /var/cache/nginx
      - /var/run
      - /tmp
```

## Related Issues

During this investigation, the following related issues were identified:

1. **No Config Option for runAsUser/runAsGroup**: The UID 1000 is hardcoded, which may conflict with images expecting different UIDs.

2. **Init Containers Also Hardcoded**: Init containers inherit the same restrictive security context, which may break migration scripts or setup tasks.

3. **Jobs/CronJobs Have Same Issue**: The `job.go` file applies the same hardcoded security context.

4. **Documentation Gap**: The README promotes "read-only filesystem" as a feature but doesn't document:
   - How to disable it
   - What images it breaks
   - Workaround with emptyDir volumes

5. **Inconsistent Handling**: Dependencies get special treatment (postgres, redis, etc. have `readOnlyRootFilesystem: false`) but user images don't have the same flexibility.

## Files Requiring Changes

1. `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/schema.go` - Add SecurityConfig struct
2. `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/render/deployment.go` - Use config value instead of hardcoded
3. `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/render/tracing.go` - Update sidecar security context
4. `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/render/job.go` - Update job security context
5. `/Users/bobbyrathore/Documents/WildProjects/kbox/README.md` - Document security config options
