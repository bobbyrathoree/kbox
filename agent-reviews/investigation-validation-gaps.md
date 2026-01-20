# Investigation: Validation Gaps

## Summary

The kbox validation system has significant gaps that allow invalid configurations to pass validation and produce incomplete or invalid Kubernetes manifests. The current validation in `/internal/config/validate.go` covers basic structural validation but lacks semantic validation for init containers, volumes, PDB, cron schedules, dependencies, and resource value bounds.

## Validation Code Location

**Primary Validation File:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/validate.go`

**Validation Entry Points:**
- `Validate(config *AppConfig)` - Main validation function called by loader
- `ValidateWithWarnings(config *AppConfig)` - Validation with non-critical warnings
- `IsValidName(name string)` - Kubernetes name format validation

**Validation Invocation:**
- `/internal/config/loader.go:57` - Called during `LoadFile()`
- `/internal/cli/validate.go` - Called by `kbox validate` command

**Multi-Service Validation:**
- `/internal/config/multiservice.go:77` - `Validate()` method on `MultiServiceConfig`

## Current Validation Coverage

### What IS Validated Today

| Field | Validation | Location |
|-------|-----------|----------|
| `apiVersion` | Must be "kbox.dev/v1" | validate.go:39-44 |
| `kind` | Must be "App" | validate.go:47-52 |
| `metadata.name` | Required, K8s naming rules | validate.go:55-65 |
| `spec.image` OR `spec.build` | At least one required | validate.go:68-73 |
| `spec.port` | 0-65535 range | validate.go:76-81 |
| `spec.replicas` | Non-negative | validate.go:84-89 |
| `spec.service.type` | ClusterIP/NodePort/LoadBalancer | validate.go:92-104 |
| `spec.ingress.host` | Required when ingress enabled | validate.go:107-114 |
| `environments.*.replicas` | Non-negative | validate.go:117-124 |
| `spec.resources.*` | Valid K8s quantity format | validate.go:128-167 |
| `spec.resources` | Request <= Limit | validate.go:145-166 |

### What IS NOT Validated (Gaps)

| Field | Current Behavior | Risk |
|-------|-----------------|------|
| `spec.initContainers[].command` | No validation | Empty command array passes |
| `spec.volumes[]` source | No validation | Volume with no source type passes |
| `spec.pdb` mutual exclusion | No validation | Both minAvailable AND maxUnavailable allowed |
| `spec.jobs[].schedule` | No validation | Invalid cron expressions pass |
| `spec.dependencies` uniqueness | No validation | Duplicate dependencies allowed |
| Resource value magnitude | No upper bound | "999999999Ti" passes validation |
| `spec.initContainers[].name` | No validation | Names not checked for K8s compliance |
| `spec.volumes[].name` | No validation | Names not checked for K8s compliance |
| `spec.jobs[].name` | No validation | Names not checked for K8s compliance |

## Gap Analysis

### Gap 1: Init Containers Without Command

**Current behavior:**
- Init containers are defined in schema (`InitContainerConfig` at schema.go:131-146)
- Command field has `yaml:"command"` tag but no `required` enforcement
- When an init container has no command, it renders to a container with `Command: nil`
- Kubernetes will run the image's default entrypoint, which may not be the intended behavior

**Evidence from render code:**
```go
// deployment.go:280-284
container := corev1.Container{
    Name:    ic.Name,
    Image:   image,
    Command: ic.Command,  // nil if not specified
    Args:    ic.Args,
}
```

**Required validation:**
```go
// In validate.go
for i, ic := range config.Spec.InitContainers {
    if len(ic.Command) == 0 {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.initContainers[%d].command", i),
            Message: "command is required for init containers",
        })
    }
    if ic.Name == "" {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.initContainers[%d].name", i),
            Message: "name is required",
        })
    } else if !IsValidName(ic.Name) {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.initContainers[%d].name", i),
            Message: "must be lowercase alphanumeric with hyphens, max 63 chars",
        })
    }
}
```

**Code location to add validation:** `/internal/config/validate.go` after line 124

---

### Gap 2: Volumes Without Source

**Current behavior:**
- Volume config has multiple source options: `Size`, `EmptyDir`, `ConfigMap`, `Secret`
- No validation that at least one source is specified
- The render code uses a switch statement that silently falls through if no source matches

**Evidence from render code:**
```go
// volume.go:73-103
switch {
case vol.Size != "":
    // PVC-backed volume
case vol.EmptyDir:
    // Ephemeral volume
case vol.ConfigMap != "":
    // ConfigMap volume
case vol.Secret != "":
    // Secret volume
}
// NO default case - volume with no source gets empty VolumeSource!
volumes = append(volumes, volume)
```

**Impact:** A volume with no source produces invalid YAML:
```yaml
volumes:
  - name: my-volume
    # No volumeSource - Kubernetes API will reject this
```

**Required validation:**
```go
// In validate.go
for i, vol := range config.Spec.Volumes {
    if vol.Name == "" {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.volumes[%d].name", i),
            Message: "name is required",
        })
    } else if !IsValidName(vol.Name) {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.volumes[%d].name", i),
            Message: "must be lowercase alphanumeric with hyphens, max 63 chars",
        })
    }

    if vol.MountPath == "" {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.volumes[%d].mountPath", i),
            Message: "mountPath is required",
        })
    }

    // Exactly one source must be specified
    sourceCount := 0
    if vol.Size != "" { sourceCount++ }
    if vol.EmptyDir { sourceCount++ }
    if vol.ConfigMap != "" { sourceCount++ }
    if vol.Secret != "" { sourceCount++ }

    if sourceCount == 0 {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.volumes[%d]", i),
            Message: "volume must specify one of: size, emptyDir, configMap, or secret",
        })
    }
    if sourceCount > 1 {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.volumes[%d]", i),
            Message: "volume can only have one source (size, emptyDir, configMap, or secret)",
        })
    }
}
```

**Code location to add validation:** `/internal/config/validate.go` after line 124

---

### Gap 3: PDB with Both minAvailable AND maxUnavailable

**Current behavior:**
- PDBConfig struct allows both fields (schema.go:157-160)
- No validation that only one is set
- Render code sets both if both are provided (pdb.go:27-34)

**Evidence from render code:**
```go
// pdb.go:27-34
if cfg.MinAvailable != "" {
    minAvail := intstr.Parse(cfg.MinAvailable)
    pdb.Spec.MinAvailable = &minAvail
}
if cfg.MaxUnavailable != "" {
    maxUnavail := intstr.Parse(cfg.MaxUnavailable)
    pdb.Spec.MaxUnavailable = &maxUnavail
}
```

**Impact:** Kubernetes API rejects PDBs with both fields set:
```
spec.minAvailable: Forbidden: must not be set when maxUnavailable is set
```

**Required validation:**
```go
// In validate.go
if config.Spec.PDB != nil {
    pdb := config.Spec.PDB
    if pdb.MinAvailable != "" && pdb.MaxUnavailable != "" {
        errs = append(errs, ValidationError{
            Field:   "spec.pdb",
            Message: "cannot specify both minAvailable and maxUnavailable (Kubernetes only allows one)",
        })
    }

    // Also validate the values are valid intstr format
    if pdb.MinAvailable != "" {
        if err := validateIntOrPercent(pdb.MinAvailable); err != nil {
            errs = append(errs, ValidationError{
                Field:   "spec.pdb.minAvailable",
                Message: fmt.Sprintf("invalid value: %v", err),
            })
        }
    }
    if pdb.MaxUnavailable != "" {
        if err := validateIntOrPercent(pdb.MaxUnavailable); err != nil {
            errs = append(errs, ValidationError{
                Field:   "spec.pdb.maxUnavailable",
                Message: fmt.Sprintf("invalid value: %v", err),
            })
        }
    }
}
```

**Code location to add validation:** `/internal/config/validate.go` after line 124

---

### Gap 4: Invalid Cron Schedules

**Current behavior:**
- Jobs with schedule field are rendered as CronJobs (job.go:16-18)
- Schedule string is passed directly to K8s without validation
- Invalid cron expressions will fail at apply time, not validation time

**Evidence from render code:**
```go
// job.go:154-156
Spec: batchv1.CronJobSpec{
    Schedule:          jc.Schedule,  // No validation!
    ConcurrencyPolicy: batchv1.ForbidConcurrent,
```

**Impact:** Invalid cron expressions like "* * *" or "every 5 minutes" pass validation but fail at apply time with cryptic errors.

**Required validation:**
```go
import "github.com/robfig/cron/v3"

// In validate.go
for i, job := range config.Spec.Jobs {
    if job.Name == "" {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.jobs[%d].name", i),
            Message: "name is required",
        })
    } else if !IsValidName(job.Name) {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.jobs[%d].name", i),
            Message: "must be lowercase alphanumeric with hyphens, max 63 chars",
        })
    }

    if len(job.Command) == 0 {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.jobs[%d].command", i),
            Message: "command is required",
        })
    }

    if job.Schedule != "" {
        // Kubernetes uses standard cron format (5 fields)
        parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
        if _, err := parser.Parse(job.Schedule); err != nil {
            errs = append(errs, ValidationError{
                Field:   fmt.Sprintf("spec.jobs[%d].schedule", i),
                Message: fmt.Sprintf("invalid cron expression: %v (format: minute hour day-of-month month day-of-week)", err),
            })
        }
    }
}
```

**Code location to add validation:** `/internal/config/validate.go` after line 124

**Note:** This would require adding `github.com/robfig/cron/v3` as a dependency, or implementing a simpler regex-based validation.

---

### Gap 5: Duplicate Dependencies

**Current behavior:**
- Dependencies array allows duplicates
- RenderAllDependencies iterates without checking for duplicates
- Duplicate deps create duplicate StatefulSets with same name

**Evidence from render code:**
```go
// dependency.go:269-297
for _, dep := range r.config.Spec.Dependencies {
    res, err := r.RenderDependency(dep)  // Each dep generates same name!
    // ...
    statefulSets = append(statefulSets, res.StatefulSet)
```

**Impact:** Duplicate dependencies with same type create naming conflicts:
```yaml
dependencies:
  - type: postgres
  - type: postgres  # Both create "myapp-postgres" StatefulSet
```

**Required validation:**
```go
// In validate.go
seenDeps := make(map[string]bool)
for i, dep := range config.Spec.Dependencies {
    if dep.Type == "" {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.dependencies[%d].type", i),
            Message: "type is required",
        })
        continue
    }

    if seenDeps[dep.Type] {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.dependencies[%d]", i),
            Message: fmt.Sprintf("duplicate dependency type %q", dep.Type),
        })
    }
    seenDeps[dep.Type] = true

    // Validate supported types
    validTypes := map[string]bool{
        "postgres": true, "mysql": true, "mongodb": true, "redis": true,
    }
    if !validTypes[dep.Type] {
        errs = append(errs, ValidationError{
            Field:   fmt.Sprintf("spec.dependencies[%d].type", i),
            Message: fmt.Sprintf("unsupported dependency type %q", dep.Type),
        })
    }
}
```

**Code location to add validation:** `/internal/config/validate.go` after line 124

---

### Gap 6: Absurdly Large Resource Values

**Current behavior:**
- Resource quantities are validated for format using `resource.ParseQuantity()`
- No upper bound validation - "999999999Ti" parses successfully
- These values will cause scheduler issues or cluster-wide problems

**Evidence from validate code:**
```go
// validate.go:176-186
func validateQuantity(value, field string) *ValidationError {
    if value == "" {
        return nil
    }
    if _, err := resource.ParseQuantity(value); err != nil {
        return &ValidationError{...}
    }
    return nil  // No bounds checking!
}
```

**Impact:** Users can accidentally specify huge values that pass validation but cause runtime issues.

**Required validation:**
```go
// Add bounds checking
var (
    maxMemory = resource.MustParse("1Ti")   // 1 Tebibyte
    maxCPU    = resource.MustParse("1000")  // 1000 cores
)

func validateQuantity(value, field string) *ValidationError {
    if value == "" {
        return nil
    }

    qty, err := resource.ParseQuantity(value)
    if err != nil {
        return &ValidationError{
            Field:   field,
            Message: fmt.Sprintf("invalid Kubernetes quantity %q: %v", value, err),
        }
    }

    // Check for reasonable bounds
    if strings.Contains(field, "memory") {
        if qty.Cmp(maxMemory) > 0 {
            return &ValidationError{
                Field:   field,
                Message: fmt.Sprintf("memory value %q exceeds maximum allowed (1Ti)", value),
            }
        }
    }
    if strings.Contains(field, "cpu") {
        if qty.Cmp(maxCPU) > 0 {
            return &ValidationError{
                Field:   field,
                Message: fmt.Sprintf("cpu value %q exceeds maximum allowed (1000)", value),
            }
        }
    }

    return nil
}
```

**Code location to modify:** `/internal/config/validate.go:176-186`

---

## Additional Gaps Found

### 1. Missing Validation for Autoscaling Config

**Issue:** No validation of HPA configuration
```go
// Should validate:
// - minReplicas > 0
// - maxReplicas > minReplicas
// - targetCPUUtilization in valid range (1-100)
```

### 2. Missing Validation for Tracing Config

**Issue:** TracingConfig has backend and samplingRate without validation
```go
// Should validate:
// - backend is "jaeger" or "zipkin"
// - samplingRate is between 0.0 and 1.0
```

### 3. Missing Validation for Metrics Config

**Issue:** MetricsConfig interval format not validated
```go
// Should validate:
// - interval is valid duration (e.g., "30s", "1m")
// - path starts with "/"
```

### 4. No Duplicate Name Checking for Volumes

**Issue:** Multiple volumes can have the same name, causing K8s conflicts
```yaml
volumes:
  - name: data
    size: 10Gi
    mountPath: /data
  - name: data        # Duplicate!
    emptyDir: true
    mountPath: /tmp
```

### 5. No Duplicate Name Checking for InitContainers

**Issue:** Multiple init containers can have the same name
```yaml
initContainers:
  - name: init
    command: ["echo", "1"]
  - name: init        # Duplicate!
    command: ["echo", "2"]
```

### 6. No Duplicate Name Checking for Jobs

**Issue:** Multiple jobs can have the same name
```yaml
jobs:
  - name: migrate
    command: ["migrate"]
  - name: migrate     # Duplicate!
    command: ["seed"]
```

### 7. Build Config Validation Missing

**Issue:** Build configuration paths not validated
```go
// Should validate:
// - context path exists (when validating --strict)
// - dockerfile path exists (when validating --strict)
```

### 8. Multi-Service DependsOn Duplicates

**Issue:** `dependsOn` array can contain duplicates
```yaml
services:
  api:
    dependsOn:
      - db
      - db    # Duplicate not caught
```

---

## Proposed Implementation

### Priority 1 - Security/Correctness Critical

1. **PDB mutual exclusion validation** - Will cause K8s API rejection
2. **Volume source validation** - Will cause K8s API rejection
3. **Duplicate dependency validation** - Will cause resource conflicts

### Priority 2 - User Experience Critical

4. **Cron schedule validation** - Saves debugging time
5. **Init container command validation** - Prevents silent failures
6. **Resource value bounds** - Prevents cluster-wide issues

### Priority 3 - Completeness

7. **Name uniqueness for volumes/initContainers/jobs**
8. **Autoscaling config validation**
9. **Tracing/Metrics config validation**
10. **Build config path validation (--strict mode only)**

### Implementation Approach

```go
// Suggested refactoring in validate.go

func Validate(config *AppConfig) error {
    var errs ValidationErrors

    // Existing validations...
    errs = append(errs, validateBasicFields(config)...)
    errs = append(errs, validateResources(config)...)

    // New validations
    errs = append(errs, validateInitContainers(config)...)
    errs = append(errs, validateVolumes(config)...)
    errs = append(errs, validatePDB(config)...)
    errs = append(errs, validateJobs(config)...)
    errs = append(errs, validateDependencies(config)...)
    errs = append(errs, validateAutoscaling(config)...)
    errs = append(errs, validateTracing(config)...)
    errs = append(errs, validateMetrics(config)...)

    if len(errs) > 0 {
        return errs
    }
    return nil
}
```

### Test Cases to Add

```go
// validate_test.go additions

func TestValidate_InitContainerWithoutCommand(t *testing.T) { ... }
func TestValidate_VolumeWithoutSource(t *testing.T) { ... }
func TestValidate_VolumeWithMultipleSources(t *testing.T) { ... }
func TestValidate_PDBBothMinAndMax(t *testing.T) { ... }
func TestValidate_InvalidCronSchedule(t *testing.T) { ... }
func TestValidate_DuplicateDependencies(t *testing.T) { ... }
func TestValidate_ExcessiveResourceValues(t *testing.T) { ... }
func TestValidate_DuplicateVolumeNames(t *testing.T) { ... }
func TestValidate_DuplicateInitContainerNames(t *testing.T) { ... }
func TestValidate_DuplicateJobNames(t *testing.T) { ... }
func TestValidate_AutoscalingMinGreaterThanMax(t *testing.T) { ... }
func TestValidate_TracingInvalidBackend(t *testing.T) { ... }
func TestValidate_TracingSamplingRateOutOfRange(t *testing.T) { ... }
```

---

## Files to Modify

| File | Changes |
|------|---------|
| `/internal/config/validate.go` | Add all new validation functions |
| `/internal/config/validate_test.go` | Add test cases for each new validation |
| `/internal/config/multiservice.go` | Add duplicate dependsOn validation |
| `go.mod` | Add `github.com/robfig/cron/v3` for cron validation (optional) |
