# Investigation: Autoscaling Validation Bug

## Summary

The autoscaling configuration (`spec.autoscaling`) has **zero validation** in kbox. Users can specify invalid configurations such as `minReplicas > maxReplicas` or out-of-bounds `targetCPUUtilization` values, which will pass kbox validation but cause Kubernetes API rejection at apply time. This creates a poor user experience where errors are discovered late in the deployment process.

## Code Location

| Component | File | Description |
|-----------|------|-------------|
| **Validation code** | `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/validate.go` | Main validation logic - **no autoscaling validation present** |
| **Schema definition** | `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/schema.go` | `AutoscalingConfig` struct (lines 148-154) |
| **HPA rendering** | `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/render/hpa.go` | `RenderHPA()` function (lines 10-61) |
| **Validation tests** | `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/validate_test.go` | **No autoscaling tests exist** |

## Current Validation

### What IS Currently Validated

The `Validate()` function in `validate.go` validates:
- `apiVersion` - must be `kbox.dev/v1`
- `kind` - must be `App`
- `metadata.name` - required, must be valid Kubernetes name (lowercase alphanumeric with hyphens, max 63 chars)
- `spec.image` - required unless `spec.build` is provided
- `spec.port` - must be between 0 and 65535
- `spec.replicas` - must be non-negative
- `spec.service.type` - must be ClusterIP, NodePort, or LoadBalancer
- `spec.ingress.host` - required when ingress is enabled
- `spec.resources.*` - validates Kubernetes quantity format
- `spec.resources` - validates request <= limit for memory and CPU
- `environments.*.replicas` - must be non-negative

### What IS NOT Validated for Autoscaling

**NONE** of the autoscaling fields are validated. The `spec.autoscaling` section is completely ignored by the validation logic.

## Missing Validation

### Critical Missing Validations

1. **minReplicas > maxReplicas** (reported bug)
   - Current: No check
   - Kubernetes will reject: `HorizontalPodAutoscaler.spec.minReplicas: Invalid value: 10: must be less than or equal to maxReplicas`

2. **minReplicas <= 0**
   - Current: No check (render defaults to 1 if 0)
   - Kubernetes requirement: `minReplicas` must be >= 1

3. **maxReplicas <= 0**
   - Current: No check (render defaults to 10 if 0)
   - Kubernetes requirement: `maxReplicas` must be >= 1

4. **targetCPUUtilization bounds**
   - Current: No check (render defaults to 80 if 0)
   - Reasonable bounds: 1-100 (percentages)
   - Values > 100 are technically valid in K8s but indicate misconfiguration
   - Values <= 0 are invalid

5. **maxReplicas too large**
   - Kubernetes allows up to 2^31-1, but extremely high values may indicate user error
   - Warning threshold could be set at 1000+ replicas

### Schema Definition (for reference)

```go
// AutoscalingConfig defines HPA settings
type AutoscalingConfig struct {
    Enabled              bool `yaml:"enabled" json:"enabled"`
    MinReplicas          int  `yaml:"minReplicas,omitempty" json:"minReplicas,omitempty"`
    MaxReplicas          int  `yaml:"maxReplicas" json:"maxReplicas"`
    TargetCPUUtilization int  `yaml:"targetCPUUtilization,omitempty" json:"targetCPUUtilization,omitempty"`
}
```

### HPA Rendering Logic (shows defaults)

From `hpa.go`:
```go
minReplicas := int32(cfg.MinReplicas)
if minReplicas == 0 {
    minReplicas = 1  // Default: 1
}
maxReplicas := int32(cfg.MaxReplicas)
if maxReplicas == 0 {
    maxReplicas = 10  // Default: 10
}
targetCPU := int32(cfg.TargetCPUUtilization)
if targetCPU == 0 {
    targetCPU = 80  // Default: 80%
}
```

## Kubernetes HPA Requirements

Based on Kubernetes HPA validation (autoscaling/v2):

| Field | Kubernetes Requirement |
|-------|----------------------|
| `minReplicas` | Must be >= 1 |
| `maxReplicas` | Must be >= 1 |
| `minReplicas` vs `maxReplicas` | minReplicas must be <= maxReplicas |
| `targetCPUUtilization` | Must be > 0 (percentage) |

### Kubernetes Error Messages

When applying invalid HPA configurations, Kubernetes returns:

```
# minReplicas > maxReplicas
The HorizontalPodAutoscaler "myapp" is invalid: spec.minReplicas: Invalid value: 10: must be less than or equal to maxReplicas

# minReplicas < 1
The HorizontalPodAutoscaler "myapp" is invalid: spec.minReplicas: Invalid value: 0: must be greater than or equal to 1

# maxReplicas < 1
The HorizontalPodAutoscaler "myapp" is invalid: spec.maxReplicas: Invalid value: 0: must be greater than or equal to 1

# targetCPUUtilization <= 0
The HorizontalPodAutoscaler "myapp" is invalid: spec.metrics[0].resource.target.averageUtilization: Invalid value: 0: must be greater than 0
```

## Proposed Fix

### Add Autoscaling Validation to `validate.go`

Insert after the environments validation (around line 124):

```go
// Validate autoscaling configuration
if config.Spec.Autoscaling != nil && config.Spec.Autoscaling.Enabled {
    as := config.Spec.Autoscaling

    // minReplicas must be >= 1 (if specified)
    if as.MinReplicas < 0 {
        errs = append(errs, ValidationError{
            Field:   "spec.autoscaling.minReplicas",
            Message: "must be non-negative",
        })
    }

    // maxReplicas must be >= 1
    if as.MaxReplicas < 1 {
        errs = append(errs, ValidationError{
            Field:   "spec.autoscaling.maxReplicas",
            Message: "must be at least 1",
        })
    }

    // minReplicas must be <= maxReplicas
    // Only check if both are positive to avoid confusing compound errors
    minRep := as.MinReplicas
    if minRep == 0 {
        minRep = 1 // Account for default
    }
    maxRep := as.MaxReplicas
    if maxRep == 0 {
        maxRep = 10 // Account for default
    }
    if minRep > maxRep {
        errs = append(errs, ValidationError{
            Field:   "spec.autoscaling",
            Message: fmt.Sprintf("minReplicas (%d) cannot exceed maxReplicas (%d)", minRep, maxRep),
        })
    }

    // targetCPUUtilization must be > 0 and <= 100 (if specified)
    if as.TargetCPUUtilization < 0 {
        errs = append(errs, ValidationError{
            Field:   "spec.autoscaling.targetCPUUtilization",
            Message: "must be non-negative",
        })
    } else if as.TargetCPUUtilization > 100 {
        errs = append(errs, ValidationError{
            Field:   "spec.autoscaling.targetCPUUtilization",
            Message: "must be between 1 and 100 (percentage)",
        })
    }
}
```

### Optional: Add Warning for Potentially Problematic Values

In `ValidateWithWarnings()`:

```go
// Autoscaling warnings
if config.Spec.Autoscaling != nil && config.Spec.Autoscaling.Enabled {
    as := config.Spec.Autoscaling
    if as.MaxReplicas > 100 {
        warnings = append(warnings, fmt.Sprintf(
            "autoscaling.maxReplicas is set to %d - ensure cluster has capacity",
            as.MaxReplicas))
    }
    if as.TargetCPUUtilization > 0 && as.TargetCPUUtilization < 20 {
        warnings = append(warnings, fmt.Sprintf(
            "autoscaling.targetCPUUtilization of %d%% is very low - may cause excessive scaling",
            as.TargetCPUUtilization))
    }
}
```

## Test Cases

Add to `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/validate_test.go`:

```go
// Tests for autoscaling validation
func TestValidate_AutoscalingMinExceedsMax(t *testing.T) {
    config := &AppConfig{
        APIVersion: DefaultAPIVersion,
        Kind:       DefaultKind,
        Metadata:   Metadata{Name: "myapp"},
        Spec: AppSpec{
            Image: "myapp:v1",
            Autoscaling: &AutoscalingConfig{
                Enabled:     true,
                MinReplicas: 10,
                MaxReplicas: 5,
            },
        },
    }

    err := Validate(config)
    if err == nil {
        t.Error("expected error for minReplicas > maxReplicas")
    }
    if !strings.Contains(err.Error(), "minReplicas") && !strings.Contains(err.Error(), "maxReplicas") {
        t.Errorf("expected error about minReplicas/maxReplicas, got: %v", err)
    }
}

func TestValidate_AutoscalingNegativeMinReplicas(t *testing.T) {
    config := &AppConfig{
        APIVersion: DefaultAPIVersion,
        Kind:       DefaultKind,
        Metadata:   Metadata{Name: "myapp"},
        Spec: AppSpec{
            Image: "myapp:v1",
            Autoscaling: &AutoscalingConfig{
                Enabled:     true,
                MinReplicas: -1,
                MaxReplicas: 10,
            },
        },
    }

    err := Validate(config)
    if err == nil {
        t.Error("expected error for negative minReplicas")
    }
}

func TestValidate_AutoscalingZeroMaxReplicas(t *testing.T) {
    // Zero maxReplicas should use default (10), not error
    // But explicit check: if user sets maxReplicas: 0 with minReplicas > 0
    config := &AppConfig{
        APIVersion: DefaultAPIVersion,
        Kind:       DefaultKind,
        Metadata:   Metadata{Name: "myapp"},
        Spec: AppSpec{
            Image: "myapp:v1",
            Autoscaling: &AutoscalingConfig{
                Enabled:     true,
                MinReplicas: 5,
                MaxReplicas: 0, // defaults to 10, so 5 <= 10 is valid
            },
        },
    }

    err := Validate(config)
    if err != nil {
        t.Errorf("expected valid config (maxReplicas defaults to 10), got: %v", err)
    }
}

func TestValidate_AutoscalingInvalidTargetCPU(t *testing.T) {
    tests := []struct {
        name      string
        targetCPU int
        wantErr   bool
    }{
        {"valid 80%", 80, false},
        {"valid 100%", 100, false},
        {"valid 1%", 1, false},
        {"zero (uses default)", 0, false},
        {"negative", -10, true},
        {"over 100%", 150, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            config := &AppConfig{
                APIVersion: DefaultAPIVersion,
                Kind:       DefaultKind,
                Metadata:   Metadata{Name: "myapp"},
                Spec: AppSpec{
                    Image: "myapp:v1",
                    Autoscaling: &AutoscalingConfig{
                        Enabled:              true,
                        MinReplicas:          1,
                        MaxReplicas:          10,
                        TargetCPUUtilization: tt.targetCPU,
                    },
                },
            }

            err := Validate(config)
            hasErr := err != nil
            if hasErr != tt.wantErr {
                t.Errorf("targetCPU=%d: wantErr=%v, gotErr=%v (%v)", tt.targetCPU, tt.wantErr, hasErr, err)
            }
        })
    }
}

func TestValidate_ValidAutoscalingConfig(t *testing.T) {
    config := &AppConfig{
        APIVersion: DefaultAPIVersion,
        Kind:       DefaultKind,
        Metadata:   Metadata{Name: "myapp"},
        Spec: AppSpec{
            Image: "myapp:v1",
            Autoscaling: &AutoscalingConfig{
                Enabled:              true,
                MinReplicas:          2,
                MaxReplicas:          10,
                TargetCPUUtilization: 75,
            },
        },
    }

    if err := Validate(config); err != nil {
        t.Errorf("expected valid autoscaling config, got error: %v", err)
    }
}

func TestValidate_AutoscalingDisabledSkipsValidation(t *testing.T) {
    // When autoscaling is disabled, invalid values should be ignored
    config := &AppConfig{
        APIVersion: DefaultAPIVersion,
        Kind:       DefaultKind,
        Metadata:   Metadata{Name: "myapp"},
        Spec: AppSpec{
            Image: "myapp:v1",
            Autoscaling: &AutoscalingConfig{
                Enabled:     false,
                MinReplicas: 100,
                MaxReplicas: 5, // Would be invalid if enabled
            },
        },
    }

    if err := Validate(config); err != nil {
        t.Errorf("expected disabled autoscaling to skip validation, got error: %v", err)
    }
}
```

### Integration Test

Add to `/Users/bobbyrathore/Documents/WildProjects/kbox/test/integration/validation_test.go`:

```go
// TestAutoscalingMinExceedsMaxRejected tests autoscaling validation
// Verifies that minReplicas > maxReplicas is rejected at validation time.
func TestAutoscalingMinExceedsMaxRejected(t *testing.T) {
    appName := "autoscale-invalid"

    kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: autoscale-invalid
  namespace: ` + TestNamespace + `
spec:
  image: alpine:3.18
  port: 8080
  autoscaling:
    enabled: true
    minReplicas: 10
    maxReplicas: 5`

    dir := createTestApp(t, appName, "", kboxYaml, "")

    // Deploy should fail validation
    _, stderr, err := runKbox(t, dir, "deploy", "--dry-run")
    if err == nil {
        t.Fatal("Expected validation error for minReplicas > maxReplicas")
    }

    assertContains(t, stderr, "minReplicas")
}
```

## Additional Considerations

### Environment-Specific Autoscaling Overrides

The schema does not currently support environment-specific autoscaling overrides in `EnvOverride`. If this feature is added in the future, validation should also cover:
- Per-environment autoscaling configurations
- Consistency checks between base and override values

### Memory-Based Autoscaling

The current schema only supports CPU-based autoscaling. If memory-based autoscaling is added, validation should include:
- `targetMemoryUtilization` bounds (1-100)
- At least one target metric specified when autoscaling is enabled

### Custom Metrics

If custom metrics support is added for HPA, validation should include:
- Metric name validation
- Metric type validation (External, Object, Pods)
- Target value validation

## Priority

**Medium-High** - This bug causes deployment failures that are only discovered at Kubernetes apply time, after potentially successful build and image push steps. Early validation would save users time and provide clearer error messages.
