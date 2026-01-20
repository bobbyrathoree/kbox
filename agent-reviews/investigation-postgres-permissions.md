# Investigation: Postgres Dependency Permission Bug

## Summary

The postgres dependency fails because kbox sets `runAsUser: 70` and `fsGroup: 70` but does not ensure the PVC has proper ownership before postgres initdb runs. When Kubernetes mounts the PVC, it may be owned by root, and while `fsGroup` should fix this, the data directory path `/var/lib/postgresql/data` creates a specific issue where postgres requires a subdirectory (`/var/lib/postgresql/data/pgdata`) to avoid initdb failures when the mount point itself is a volume root.

## Code Location

The postgres StatefulSet rendering code is located in:

- **`/Users/bobbyrathore/Documents/WildProjects/kbox/internal/render/dependency.go`** - Main rendering logic
  - `RenderDependency()` (line 32) - Creates StatefulSet with security context
  - `dependencyPodSecurityContext()` (line 322) - Sets pod-level security (runAsUser, fsGroup)
  - `dependencySecurityContext()` (line 304) - Sets container-level security
  - `getDataPath()` (line 375) - Returns data directory paths

- **`/Users/bobbyrathore/Documents/WildProjects/kbox/internal/dependencies/registry.go`** - Dependency templates
  - Line 51-69: Postgres template definition with image `postgres:15-alpine`

## Root Cause

The bug has multiple contributing factors:

### 1. Incorrect UID for Standard Postgres Image

The code sets `runAsUser: 70` (line 331) assuming the Alpine image UID:
```go
case "postgres":
    // Postgres runs as uid 70 (postgres user in alpine image)
    uid := int64(70)
```

However, the default image `postgres:15-alpine` actually does use UID 70, so this is correct. The problem lies elsewhere.

### 2. PGDATA Path Issue (Primary Cause)

The critical issue is in `getDataPath()`:
```go
case "postgres":
    return "/var/lib/postgresql/data"
```

When a PVC is mounted directly at `/var/lib/postgresql/data`, postgres initdb fails because:
1. The mount point directory is managed by Kubernetes (contains `lost+found` on some filesystems)
2. postgres initdb refuses to initialize in a non-empty directory
3. postgres documentation recommends using a subdirectory (e.g., `/var/lib/postgresql/data/pgdata`)

### 3. Volume Mount Timing vs. fsGroup

While `fsGroup: 70` is set, there's a race condition:
- Kubernetes sets fsGroup ownership asynchronously after mounting
- postgres initdb may run before ownership is fully propagated
- Some storage backends don't support fsGroup properly

### 4. Missing PGDATA Environment Variable

The postgres image expects `PGDATA` to be set. Without it, the container uses its default which may conflict with the mount point.

## How Postgres Helm Chart Handles This

The Bitnami PostgreSQL Helm chart uses several mechanisms:

### 1. Init Container for Permission Fixing

```yaml
initContainers:
  - name: init-chmod-data
    image: bitnami/bitnami-shell
    command:
      - /bin/bash
      - -c
      - |
        chown -R 1001:1001 /bitnami/postgresql
        chmod 700 /bitnami/postgresql/data
    securityContext:
      runAsUser: 0  # Runs as root temporarily
    volumeMounts:
      - name: data
        mountPath: /bitnami/postgresql
```

### 2. Use of fsGroup (Primary Mechanism)

```yaml
podSecurityContext:
  fsGroup: 1001
  fsGroupChangePolicy: Always  # Important: forces immediate ownership change
```

### 3. Separate Data Subdirectory

The Bitnami image uses `/bitnami/postgresql/data` as a subdirectory within the mount point `/bitnami/postgresql`, avoiding the "non-empty directory" issue.

### 4. Custom Image with UID 1001

Bitnami uses their own image with consistent UID 1001 across all containers, not the official postgres image.

## Other Dependencies Affected

| Dependency | UID in kbox | Actual Image UID | Affected? | Severity |
|------------|-------------|------------------|-----------|----------|
| **postgres** | 70 | 70 (alpine) | **YES** | High - initdb fails |
| **redis** | 999 | 999 | **Partial** | Medium - may work with fsGroup |
| **mongodb** | 999 | 999 | **Partial** | Medium - may fail on some storage |
| **mysql** | 999 | 999 | **YES** | High - similar initdb issue |

### Redis Analysis
- Redis UID is 999 with GID 1000 in alpine images
- kbox incorrectly sets GID to 999 (should be 1000)
- However, Redis is more forgiving about directory ownership
- fsGroup should work in most cases

### MongoDB Analysis
- MongoDB uses UID/GID 999
- Data path `/data/db` is correct
- May fail on storage backends that don't support fsGroup
- No initdb-style directory requirements, more resilient

### MySQL Analysis
- MySQL uses UID/GID 999 (correct in kbox)
- Similar initdb issues as postgres
- Requires empty data directory for initialization
- May fail on "lost+found" in mounted volume

## Proposed Fixes

### Option A: Init Container for Permission Fixing

Add an init container that runs as root to fix permissions:

```go
func (r *Renderer) RenderDependency(dep config.DependencyConfig) (*DependencyResources, error) {
    // ... existing code ...

    // Add init container for databases that need it
    if needsPermissionFix(dep.Type) {
        initContainer := corev1.Container{
            Name:    "fix-permissions",
            Image:   "busybox:1.36",
            Command: []string{"/bin/sh", "-c"},
            Args: []string{
                fmt.Sprintf("chown -R %d:%d %s && chmod 700 %s",
                    uid, uid, getDataPath(dep.Type), getDataPath(dep.Type)),
            },
            SecurityContext: &corev1.SecurityContext{
                RunAsUser: ptr.To(int64(0)),  // Run as root
            },
            VolumeMounts: []corev1.VolumeMount{
                {Name: "data", MountPath: getDataPath(dep.Type)},
            },
        }
        statefulSet.Spec.Template.Spec.InitContainers = append(
            statefulSet.Spec.Template.Spec.InitContainers, initContainer)
    }
}
```

**Pros:**
- Guaranteed to fix permissions before main container starts
- Works with any storage backend

**Cons:**
- Requires running as root (security concern)
- May violate PodSecurityPolicy/Standards in restricted namespaces
- Adds complexity and init container overhead

### Option B: Use Subdirectory for PGDATA

Modify the postgres configuration to use a subdirectory:

```go
func getDataPath(depType string) string {
    switch depType {
    case "postgres":
        return "/var/lib/postgresql/data/pgdata"  // Use subdirectory
    // ...
    }
}

// Also add PGDATA env var to the container
env := []corev1.EnvVar{
    {Name: "PGDATA", Value: "/var/lib/postgresql/data/pgdata"},
}
```

Mount the PVC at `/var/lib/postgresql/data` and let postgres create the `pgdata` subdirectory itself.

**Pros:**
- No root access required
- Works with official postgres image
- Simple change

**Cons:**
- Doesn't fix the fsGroup timing issue
- May still fail on problematic storage backends

### Option C: Add fsGroupChangePolicy (Kubernetes 1.23+)

Add `fsGroupChangePolicy: OnRootMismatch` or `Always`:

```go
func dependencyPodSecurityContext(depType string) *corev1.PodSecurityContext {
    // ...
    fsGroupChangePolicy := corev1.FSGroupChangeAlways

    return &corev1.PodSecurityContext{
        RunAsNonRoot:          &runAsNonRoot,
        RunAsUser:             &uid,
        RunAsGroup:            &uid,
        FSGroup:               &uid,
        FSGroupChangePolicy:   &fsGroupChangePolicy,  // Add this
        SeccompProfile:        seccompProfile,
    }
}
```

**Pros:**
- Pure Kubernetes native solution
- No init containers or root access

**Cons:**
- Requires Kubernetes 1.23+
- Doesn't solve the non-empty directory issue
- Storage backend must support fsGroup

### Option D: Combined Fix (Recommended Approach)

Combine Options B and C for maximum compatibility:

1. Use subdirectory for data path (fixes non-empty directory issue)
2. Add `fsGroupChangePolicy: OnRootMismatch` (ensures ownership is set)
3. Set PGDATA environment variable explicitly
4. Optionally add init container only when explicitly enabled via config

```go
func getDataPath(depType string) string {
    switch depType {
    case "postgres":
        return "/var/lib/postgresql/data"  // Mount point
    }
}

func getDataSubdir(depType string) string {
    switch depType {
    case "postgres":
        return "/var/lib/postgresql/data/pgdata"  // Actual data dir
    }
}

// In RenderDependency, add PGDATA env var
if depType == "postgres" {
    container.Env = append(container.Env, corev1.EnvVar{
        Name:  "PGDATA",
        Value: getDataSubdir(depType),
    })
}
```

**Pros:**
- Maximum compatibility
- No root access required by default
- Follows postgres best practices
- Works with official images

**Cons:**
- Slightly more complex implementation

## Recommended Fix

**Option D (Combined Fix)** is the recommended approach because:

1. **Security First**: Does not require root access or init containers by default
2. **Follows Best Practices**: Uses PGDATA subdirectory as recommended by postgres documentation
3. **Maximum Compatibility**: fsGroupChangePolicy ensures proper ownership on supported clusters
4. **Minimal Breaking Changes**: Existing deployments using postgres will need migration, but the approach is industry-standard

### Implementation Steps

1. **Modify `getDataPath()`** to return mount point path
2. **Add `getDataSubdir()`** function for actual data directories
3. **Add PGDATA environment variable** for postgres containers
4. **Add fsGroupChangePolicy** to pod security context
5. **Fix Redis GID** from 999 to 1000 (matches actual image)
6. **Add MySQL MYSQL_DATA_DIR** environment variable similar to PGDATA
7. **Update documentation** about volume requirements

### Migration Consideration

Existing postgres deployments will need data migration:
```bash
# On the existing pod
cp -a /var/lib/postgresql/data/* /var/lib/postgresql/data/pgdata/
```

Or provide a migration init container for users upgrading from older kbox versions.

---

**Investigation completed by:** Claude Agent
**Date:** 2026-01-21
**Files examined:**
- `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/render/dependency.go`
- `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/dependencies/registry.go`
