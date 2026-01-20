# Investigation: JSON Output Mixed Content Bug

## Summary

When using `--output json` or `-o json`, kbox commands output valid JSON to stdout but then append plain text error messages (either to stderr via cobra's error handling, or mix with stdout), breaking CI/CD pipelines that parse the output. The root cause is inconsistent handling: some commands use `os.Exit(1)` to avoid error messages while others return errors that get printed by the root command.

## Code Location

### Root Error Handling
- **File:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/root.go`
  - Lines 43-48: `Execute()` function prints errors to stderr via `fmt.Fprintln(os.Stderr, err)`
  - Lines 75-82: `GetOutputFormat()` retrieves output format
  - Lines 84-87: `NewOutputWriter()` creates output writer

### Output Module
- **File:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/output/result.go`
  - Lines 48-65: `Writer` struct with format/CI mode support
  - Lines 98-101: `WriteJSON()` method for generic JSON output

### Main Entry Point
- **File:** `/Users/bobbyrathore/Documents/WildProjects/kbox/cmd/kbox/main.go`
  - Lines 9-12: Calls `cli.Execute()` and exits with code 1 on error

## Problem Areas Found

### 1. validate.go - Primary Bug Location (Lines 78-89)
```go
// JSON output
if outputFormat == "json" {
    enc := json.NewEncoder(os.Stdout)
    enc.SetIndent("", "  ")
    if err := enc.Encode(result); err != nil {
        return err
    }
    if !result.Valid {
        return fmt.Errorf("validation failed")  // <-- BUG: This gets printed to stderr
    }
    return nil
}
```
**Problem:** After outputting JSON, returns an error that gets printed to stderr by root.go.

### 2. job.go - Similar Pattern (Lines 169-173)
```go
if completed.Status.Failed > 0 {
    return fmt.Errorf("job failed")  // <-- After JSON output, plain text error
}
```

### 3. deploy.go - Proper Pattern (Lines 64-70)
```go
if outputFormat == "json" {
    output.NewWriter(os.Stdout, outputFormat, ciMode).WriteDeployResult(result)
    if !result.Success {
        os.Exit(1)  // <-- Correct: exits without printing error text
    }
    return nil
}
```
**Note:** Deploy handles this correctly by using `os.Exit(1)` instead of returning an error.

### 4. down.go - Partial Issue (Lines 420-425)
```go
if len(errors) > 0 {
    fmt.Fprintln(os.Stderr, "\nErrors:")
    for _, e := range errors {
        fmt.Fprintf(os.Stderr, "  - %v\n", e)  // <-- Prints even in JSON mode
    }
    return fmt.Errorf("down completed with %d errors", len(errors))
}
```

### 5. render.go - Mixing stderr Messages (Lines 51-65, 80-83)
```go
if !ciMode {
    fmt.Fprintln(os.Stderr, "No kbox.yaml found, inferring from Dockerfile...")
}
```
**Problem:** Writes to stderr even when JSON output is requested (though ciMode guards help).

### 6. promote.go - No JSON Support
- Lines 132-145: Outputs plain text even when `--output json` is specified
- No JSON output implementation at all

## Commands Affected

| Command | Supports JSON | Issue |
|---------|--------------|-------|
| `validate` | Yes | Returns error after JSON output |
| `deploy` | Yes | **Correct** - uses os.Exit(1) |
| `status` | Yes | OK when successful |
| `doctor` | Yes | OK |
| `version` | Yes | OK |
| `diff` | Yes | OK |
| `history` | Yes | OK |
| `preview create` | Yes | Returns error after JSON on failure |
| `preview destroy` | Yes | Returns error after JSON on failure |
| `preview list` | Yes | OK |
| `job run` | Yes | Returns error after JSON output |
| `job list` | Yes | OK |
| `job logs` | Yes | OK |
| `dns` | Yes | OK |
| `expose` | Yes | OK |
| `unexpose` | Yes | OK |
| `down` | Yes | Prints errors to stderr even in JSON mode |
| `render` | Yes (JSON manifests) | stderr messages not suppressed |
| `promote` | No | No JSON support, plain text only |

## Industry Standards

### kubectl
- JSON output goes to **stdout only**
- Errors always go to **stderr as plain text**
- Exit code indicates success/failure
- CI pipelines check: `kubectl get pod -o json 2>/dev/null; echo $?`

### Terraform
- Uses `-json` flag for machine-readable output
- JSON output includes an `"errored": true` field
- Errors are embedded **within** the JSON structure
- Format versioning (`format_version`) for compatibility

### GitHub CLI (gh)
- Uses `--json` flag for structured output
- Errors written to stderr as plain text
- Exit codes indicate failure
- Some commands support `--jq` for filtering

### Common Pattern
Most CLI tools follow one of two approaches:
1. **Separate streams**: JSON to stdout, errors to stderr (kubectl style)
2. **Unified JSON**: All output including errors in JSON structure (Terraform style)

## Proposed Fixes

### Option A: Unified JSON Errors (Terraform Style)

Include errors within the JSON output structure. The JSON already has fields for this.

**Changes Required:**
1. All commands that support JSON should include `success` and `error` fields
2. When JSON mode is active, **never return errors** - use `os.Exit(1)` instead
3. Update validate.go, job.go, preview.go to follow deploy.go pattern

**Example Output (failure):**
```json
{
  "success": false,
  "valid": false,
  "errors": ["validation failed: missing required field 'name'"],
  "warnings": []
}
```

**Pros:**
- Parseable output in all cases
- Error details are machine-readable
- Matches existing `success` field pattern

**Cons:**
- Requires changes to multiple commands
- Consumers must check `success` field

### Option B: Clean Stream Separation (kubectl Style)

JSON to stdout only, errors to stderr, never mix.

**Changes Required:**
1. In JSON mode, suppress all stderr output (except truly fatal errors)
2. Use `os.Exit(1)` instead of returning errors in JSON mode
3. Ensure `success: false` is set in JSON before exit

**Pros:**
- Simple stdout parsing: `kbox validate -o json | jq .`
- Clear separation of concerns
- Familiar to kubectl users

**Cons:**
- Loses error details in machine-readable form
- Requires `2>/dev/null` in some scripts

### Option C: Add --quiet Flag

Add a `--quiet` or `--machine` flag that suppresses all non-JSON output.

**Changes Required:**
1. Add global `--quiet` flag
2. When `--quiet` is set, suppress all stderr/stdout except structured output
3. Combine with `--output json` for clean JSON

**Pros:**
- Backward compatible
- Explicit user intent
- Works for both JSON and other formats

**Cons:**
- Another flag to remember
- Doesn't fix the fundamental issue

## Recommended Fix

**Option A: Unified JSON Errors** is recommended for the following reasons:

1. **Consistency with existing pattern**: The codebase already uses `success` and `error` fields in JSON responses (see `output/result.go`). The bug is that errors are still printed to stderr after JSON output.

2. **CI/CD friendly**: Pipelines can parse a single JSON response to determine success/failure and get error details.

3. **Minimal user impact**: No new flags needed, behavior becomes more predictable.

4. **Clear implementation path**: Follow the pattern already established in `deploy.go`:
   ```go
   if outputFormat == "json" {
       encoder.Encode(result)  // result includes success/error fields
       if !result.Success {
           os.Exit(1)  // Exit without returning error to avoid stderr print
       }
       return nil
   }
   ```

### Implementation Steps

1. **Update validate.go** (lines 78-89):
   - Move error into result struct
   - Use `os.Exit(1)` instead of returning error

2. **Update job.go** `runJobRun` (lines 169-173):
   - Include failure info in JSON result
   - Use `os.Exit(1)` on failure

3. **Update preview.go** `runPreviewCreate` and `runPreviewDestroy`:
   - Return structured error in JSON
   - Exit cleanly after JSON output

4. **Update down.go** (lines 420-425):
   - Skip stderr printing when `outputFormat == "json"`
   - Error details already included in JSON result

5. **Add JSON support to promote.go**:
   - Define result struct
   - Implement JSON output path

6. **Documentation**:
   - Document that `--output json` guarantees parseable JSON on stdout
   - Document exit codes (0 = success, 1 = failure)
   - Note that errors are included in JSON structure

### Example Fix for validate.go

```go
// JSON output
if outputFormat == "json" {
    enc := json.NewEncoder(os.Stdout)
    enc.SetIndent("", "  ")
    if err := enc.Encode(result); err != nil {
        // Encoding error - fall through to return error
        return err
    }
    // Exit with appropriate code, don't return error (would print to stderr)
    if !result.Valid {
        os.Exit(1)
    }
    return nil
}
```

## Commands Needing Changes

1. `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/validate.go` - Lines 85-88
2. `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/job.go` - Lines 169-173
3. `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/preview.go` - Lines 76-77, 163-164, 212-213
4. `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/down.go` - Lines 420-425
5. `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/render.go` - Lines 51-65 (stderr messages)
6. `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/promote.go` - Add JSON support entirely
