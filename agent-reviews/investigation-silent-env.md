# Investigation: Silent Environment Override Bug

## Summary

When using `kbox deploy -e nonexistent` with an environment name that is not defined in the `environments:` block of kbox.yaml, the command silently succeeds and deploys the base configuration without any warning or error. This behavior is inconsistent with `kbox promote`, which correctly validates that environments exist before proceeding.

## Code Location

| File | Lines | Description |
|------|-------|-------------|
| `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/schema.go` | 449-491 | `AppConfig.ForEnvironment()` method - the root cause |
| `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/multiservice.go` | 211-263 | `MultiServiceConfig.ForEnvironment()` - same pattern |
| `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/deploy.go` | 37, 104-106, 143-145, 455-457 | Where `ForEnvironment()` is called without validation |
| `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/up.go` | 77-80 | Same issue in `kbox up` |
| `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/render.go` | 76-82, 110-115, 255-260 | Same issue in `kbox render` |
| `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/diff.go` | 54-58 | Same issue in `kbox diff` |
| `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/promote.go` | 73-77 | **Correctly validates** environment exists |

## Root Cause Analysis

The bug originates in the `ForEnvironment()` method in `schema.go` (lines 449-458):

```go
func (c *AppConfig) ForEnvironment(env string) *AppConfig {
    if env == "" || c.Environments == nil {
        return c
    }

    override, ok := c.Environments[env]
    if !ok {
        return c  // <-- BUG: Silently returns base config
    }
    // ... apply overrides
}
```

When the requested environment doesn't exist in the `Environments` map, the method silently returns the original config without any indication that the environment was not found.

### Is This Intentional?

**Partially yes, but poorly designed:**

1. **Test Evidence:** The test in `schema_test.go` (lines 80-84) explicitly tests this behavior:
   ```go
   // Test with non-existent environment
   result = config.ForEnvironment("staging")
   if result.Spec.Replicas != 1 {
       t.Errorf("expected replicas 1 for unknown env, got %d", result.Spec.Replicas)
   }
   ```
   This suggests the silent fallback was intentional, possibly to support a "base" deployment scenario.

2. **Similar test in `zeroconfig_test.go`** (lines 156-170):
   ```go
   t.Run("unknown environment returns base config", func(t *testing.T) {
       // ...
       result := cfg.ForEnvironment("nonexistent")
       // Expects base config without error
   })
   ```

3. **Inconsistency with `promote`:** The `promote` command correctly validates environments (lines 73-77):
   ```go
   envOverride, ok := cfg.Environments[targetEnv]
   if !ok {
       return fmt.Errorf("environment %q not defined in kbox.yaml\n  Add it with:...")
   }
   ```

**Conclusion:** The current behavior appears to be an original design decision that wasn't fully thought through, leading to inconsistent UX across commands.

## Current Behavior

Step-by-step what happens with `kbox deploy -e nonexistent`:

1. User runs: `kbox deploy -e nonexistent`
2. `deploy.go:37` retrieves the `env` flag value: `"nonexistent"`
3. Config is loaded from `kbox.yaml`
4. `deploy.go:143-144` calls `cfg.ForEnvironment("nonexistent")`
5. `schema.go:455-458` checks if `"nonexistent"` exists in `cfg.Environments`
6. It doesn't exist, so the method returns the original `c` (base config)
7. **No warning, no error is produced**
8. `deploy.go:209` prints `Environment: nonexistent` (misleading!)
9. Deployment proceeds with base configuration
10. User believes they deployed with "nonexistent" environment overrides

**The danger:** A typo like `kbox deploy -e prod` vs `kbox deploy -e prdo` would silently deploy production with dev/base configuration.

## Proposed Fixes

### Option A: Error on Non-Existent Environment (Strict Mode)

Return an error when the specified environment doesn't exist.

**Changes required:**
1. Modify `ForEnvironment()` to return `(*AppConfig, error)`:
   ```go
   func (c *AppConfig) ForEnvironment(env string) (*AppConfig, error) {
       if env == "" {
           return c, nil
       }
       if c.Environments == nil {
           return nil, fmt.Errorf("no environments defined; cannot use -e %s", env)
       }
       override, ok := c.Environments[env]
       if !ok {
           return nil, fmt.Errorf("environment %q not defined in kbox.yaml", env)
       }
       // ... apply overrides
       return &result, nil
   }
   ```

2. Update all callers (deploy.go, up.go, render.go, diff.go) to handle the error.

**Pros:**
- Catches typos immediately
- Consistent with `promote` command behavior
- Prevents accidental wrong deployments

**Cons:**
- Breaking change for users who rely on silent fallback
- Requires updating all commands that use `ForEnvironment()`

### Option B: Warn But Continue

Print a warning when environment doesn't exist but proceed with base config.

**Changes required:**
1. Modify `ForEnvironment()` to return a boolean indicating if environment was found:
   ```go
   func (c *AppConfig) ForEnvironment(env string) (*AppConfig, bool) {
       if env == "" || c.Environments == nil {
           return c, false
       }
       override, ok := c.Environments[env]
       if !ok {
           return c, false
       }
       // ... apply overrides
       return &result, true
   }
   ```

2. Add warning in CLI commands:
   ```go
   cfg, found := cfg.ForEnvironment(env)
   if env != "" && !found {
       fmt.Fprintf(os.Stderr, "Warning: environment %q not defined, using base config\n", env)
   }
   ```

**Pros:**
- Non-breaking change
- Users are informed of the situation
- Allows intentional "base" deployments

**Cons:**
- Easy to miss warnings in CI logs
- Doesn't prevent the mistake, just informs about it

### Option C: Allow Defined Environments + "base" Keyword

Make "base" a special keyword that explicitly means "no overlay".

**Changes required:**
1. Accept "base" as a valid environment that applies no overrides
2. Error on any other non-existent environment

```go
func (c *AppConfig) ForEnvironment(env string) (*AppConfig, error) {
    if env == "" || env == "base" {
        return c, nil
    }
    if c.Environments == nil {
        return nil, fmt.Errorf("no environments defined; use -e base for base config")
    }
    override, ok := c.Environments[env]
    if !ok {
        return nil, fmt.Errorf("environment %q not defined (available: %v, or use 'base')",
            env, keys(c.Environments))
    }
    // ... apply overrides
}
```

**Pros:**
- Clear semantic meaning
- Catches typos
- Provides escape hatch for intentional base deployment

**Cons:**
- "base" becomes a reserved word
- Slightly more complex logic

### Option D: Add --strict Flag

Add a `--strict` flag to enable environment validation.

```go
deployCmd.Flags().Bool("strict", false, "Error if environment doesn't exist")
```

**Pros:**
- Fully backwards compatible
- Opt-in safety

**Cons:**
- Extra flag to remember
- Default behavior remains unsafe
- Inconsistent with `promote` which is always strict

## Recommended Fix

**Option A: Error on Non-Existent Environment** is the recommended approach.

### Rationale:

1. **Consistency:** Aligns with existing behavior in `kbox promote`
2. **Safety:** Prevents the most dangerous scenario (production deployment with wrong config)
3. **Clear Feedback:** Users immediately know if they've made a typo
4. **Low Migration Cost:** Users can update their CI/CD pipelines quickly
5. **UX Best Practice:** Silent failures are generally considered bad UX

### Migration Path:

1. Add the validation in `ForEnvironment()` returning an error
2. Update all CLI commands to handle the error
3. Document the change in release notes
4. For users who want "base" deployment, they can simply omit the `-e` flag

### Alternative for Backwards Compatibility:

If strict backwards compatibility is required, implement **Option C** (base keyword) which provides both safety and an explicit way to use base config.

## Test Cases Needed

### Unit Tests (schema_test.go)

1. **TestForEnvironment_NonExistentReturnsError**
   - Call `ForEnvironment("nonexistent")`
   - Expect error containing "not defined"

2. **TestForEnvironment_EmptyStringReturnsBaseNoError**
   - Call `ForEnvironment("")`
   - Expect base config, no error

3. **TestForEnvironment_NilEnvironmentsMapReturnsError**
   - Config with no `Environments` map
   - Call `ForEnvironment("prod")`
   - Expect descriptive error

4. **TestForEnvironment_ValidEnvironmentAppliesOverrides**
   - Existing test, ensure it still passes

### Integration Tests (CLI)

5. **TestDeployNonExistentEnvironmentFails**
   - Create kbox.yaml with `environments: {prod: {...}}`
   - Run `kbox deploy -e staging`
   - Expect non-zero exit code
   - Expect error message containing "staging" and "not defined"

6. **TestDeployValidEnvironmentSucceeds**
   - Create kbox.yaml with `environments: {prod: {...}}`
   - Run `kbox deploy -e prod --dry-run`
   - Expect zero exit code

7. **TestDeployNoEnvironmentFlagUsesBase**
   - Create kbox.yaml with environments
   - Run `kbox deploy --dry-run` (no -e flag)
   - Expect zero exit code, base config used

8. **TestUpNonExistentEnvironmentFails**
   - Same as test 5 but for `kbox up`

9. **TestRenderNonExistentEnvironmentFails**
   - Same as test 5 but for `kbox render`

10. **TestDiffNonExistentEnvironmentFails**
    - Same as test 5 but for `kbox diff`

### Edge Cases

11. **TestEnvironmentNameCaseSensitive**
    - Environments defined: `{Prod: {...}}`
    - Call with `-e prod` (lowercase)
    - Expect error (case-sensitive matching)

12. **TestEnvironmentNameWithSpecialChars**
    - Define environment `"prod-us-east-1"`
    - Ensure it works correctly

13. **TestMultiServiceConfigSameBehavior**
    - Same tests for `MultiServiceConfig.ForEnvironment()`
