# Investigation: kbox doctor Exit Code Bug

## Summary
The `kbox doctor` command always returns `nil` (exit code 0) regardless of whether checks pass or fail. Additionally, the code treats optional tools (kind, sops) the same as required tools when determining `hasErrors`, causing false positives in failure detection.

## Code Location
- **File:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/doctor.go`
- **Function:** `runDoctor` (lines 33-160)
- **Exit flow:** `cmd/kbox/main.go` calls `cli.Execute()` which returns errors from `RunE` functions. Exit code 1 is set only when an error is returned.

## Current Behavior

### Exit Code Logic (lines 117-159)
```go
// Check for errors
hasErrors := false
for _, r := range results {
    if !r.ok {
        hasErrors = true
        break
    }
}
// ... output formatting ...
return nil  // <-- ALWAYS returns nil, regardless of hasErrors
```

The function:
1. Correctly identifies when checks fail (`hasErrors = true`)
2. Prints appropriate messages ("Some checks failed" vs "All checks passed")
3. **Bug:** Always returns `nil`, so exit code is always 0

### Tool Classification Issue (lines 42-48)
```go
// Check required tools
results = append(results, checkTool("docker", "required for building images"))
results = append(results, checkTool("kubectl", "required for cluster operations"))

// Check optional tools
results = append(results, checkTool("kind", "optional, for local clusters"))
results = append(results, checkTool("sops", "optional, for encrypted secrets"))
```

The comments distinguish required vs optional, but the `checkResult` struct has no field to track this distinction. All failures are treated equally in `hasErrors` computation.

## Checks Analysis

| Check | Required? | Current Exit | Should Exit |
|-------|-----------|--------------|-------------|
| docker | Yes | 0 (always) | 1 if missing |
| kubectl | Yes | 0 (always) | 1 if missing |
| kind | No | 0 (always) | 0 (optional) |
| sops | No | 0 (always) | 0 (optional) |
| kubeconfig | Yes | 0 (always) | 1 if missing |
| cluster connection | Yes | 0 (always) | 1 if fails |
| namespace | Yes | 0 (always) | 1 if no access |
| permission: create deployments | Yes | 0 (always) | 1 if denied |
| permission: create services | Yes | 0 (always) | 1 if denied |
| permission: create configmaps | Yes | 0 (always) | 1 if denied |
| permission: create pods/exec | Yes | 0 (always) | 1 if denied |

## Proposed Fix

### 1. Add `required` field to `checkResult` struct

```go
type checkResult struct {
    name     string
    ok       bool
    message  string
    required bool  // NEW: distinguish required vs optional
}
```

### 2. Update `checkTool` function to accept `required` parameter

```go
func checkTool(name, description string, required bool) checkResult {
    // ... existing logic ...
    return checkResult{
        name:     name,
        ok:       true,  // or false
        message:  msg,
        required: required,
    }
}
```

### 3. Update check calls

```go
// Check required tools
results = append(results, checkTool("docker", "required for building images", true))
results = append(results, checkTool("kubectl", "required for cluster operations", true))

// Check optional tools
results = append(results, checkTool("kind", "optional, for local clusters", false))
results = append(results, checkTool("sops", "optional, for encrypted secrets", false))
```

### 4. Fix exit code logic

```go
// Check for required check failures only
hasRequiredErrors := false
for _, r := range results {
    if !r.ok && r.required {
        hasRequiredErrors = true
        break
    }
}

// ... output formatting ...

if hasRequiredErrors {
    return fmt.Errorf("required checks failed")
}
return nil
```

### 5. Update output to distinguish required vs optional failures

```go
for _, r := range results {
    status := "✓"
    if !r.ok {
        if r.required {
            status = "✗"  // Required failure
        } else {
            status = "○"  // Optional missing (not an error)
        }
    }
    fmt.Printf("  %s %s: %s\n", status, r.name, r.message)
}
```

## Additional Considerations

1. **JSON output:** The `success` field in JSON output should also only reflect required check failures.

2. **Test coverage:** The existing test at `test/integration/debug_test.go:102` does not verify exit codes (it ignores the error return from `runKbox`). Tests should be added to verify:
   - Exit code 0 when all required checks pass (even if optional tools missing)
   - Exit code 1 when any required check fails

3. **CI mode:** Consider adding a `--strict` flag that treats all failures (including optional) as errors for CI environments that want comprehensive validation.
