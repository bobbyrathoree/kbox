# Investigation: kbox init Spaces Bug

## Summary

Running `kbox init` in a directory with spaces in its name (e.g., "my app") creates a `kbox.yaml` with an invalid Kubernetes name. The generated config fails validation when loaded by subsequent commands because the name is taken directly from `filepath.Base(workDir)` without any validation or sanitization.

## Code Location

**Primary location:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/init.go`

Line 156-157 sets the name from the directory:
```go
if cfg.Metadata.Name == "" {
    cfg.Metadata.Name = filepath.Base(workDir)
}
```

**Secondary location:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/loader.go`

Line 140 in `InferFromDockerfile()` also uses directory name without sanitization:
```go
name := filepath.Base(absDir)
```

## Current Behavior

1. User runs `kbox init` in directory `/path/to/my app`
2. `filepath.Base(workDir)` returns `"my app"` (with space)
3. Config is created with `metadata.name: "my app"`
4. `kbox.yaml` is written to disk successfully
5. User runs `kbox deploy` or any other command
6. Config loader calls `Validate()` which calls `IsValidName()`
7. `IsValidName()` returns `false` because space is not in `[a-z0-9-]`
8. Validation error: `"metadata.name: must be lowercase alphanumeric with hyphens, max 63 chars"`

The bug exists because `kbox init` writes the YAML without calling `Validate()` first.

## Kubernetes Name Requirements

Kubernetes uses **DNS-1123** naming conventions for most resources:

| Rule | Description |
|------|-------------|
| Length | Maximum 63 characters (label) or 253 characters (subdomain) |
| Characters | Only lowercase letters `a-z`, digits `0-9`, and hyphens `-` |
| Start | Must start with a lowercase letter |
| End | Must end with an alphanumeric character (not hyphen) |

**Invalid characters in common directory names:**
- Spaces: `my app` -> invalid
- Underscores: `my_app` -> invalid
- Uppercase: `MyApp` -> invalid
- Dots (for labels): `my.app` -> invalid for most K8s names

The existing `IsValidName()` function at `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/validate.go:210-235` correctly implements these rules.

## Proposed Fixes

### Option A: Sanitize automatically

Add a `SanitizeName()` function that transforms invalid names into valid ones:

```go
func SanitizeName(name string) string {
    // Convert to lowercase
    name = strings.ToLower(name)

    // Replace common invalid chars with hyphens
    name = strings.ReplaceAll(name, " ", "-")
    name = strings.ReplaceAll(name, "_", "-")
    name = strings.ReplaceAll(name, ".", "-")

    // Remove any remaining invalid characters
    var result strings.Builder
    for _, c := range name {
        if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
            result.WriteRune(c)
        }
    }
    name = result.String()

    // Ensure starts with letter (prepend 'app-' if needed)
    if len(name) > 0 && (name[0] < 'a' || name[0] > 'z') {
        name = "app-" + name
    }

    // Remove trailing hyphens
    name = strings.TrimRight(name, "-")

    // Truncate to 63 chars
    if len(name) > 63 {
        name = strings.TrimRight(name[:63], "-")
    }

    // Fallback for empty result
    if name == "" {
        name = "app"
    }

    return name
}
```

**Pros:**
- Zero friction for users
- Works automatically with any directory name
- Common pattern in CLI tools (Docker, Helm)

**Cons:**
- May produce unexpected names
- User may not notice the name was changed
- `"My Cool App"` becomes `"my-cool-app"` which may confuse users

### Option B: Error with suggestion

Validate the name and error with a helpful message:

```go
if cfg.Metadata.Name == "" {
    cfg.Metadata.Name = filepath.Base(workDir)
}

if !IsValidName(cfg.Metadata.Name) {
    suggested := SanitizeName(cfg.Metadata.Name)
    return fmt.Errorf("directory name %q is not a valid Kubernetes name\n"+
        "  Names must be lowercase alphanumeric with hyphens (DNS-1123)\n"+
        "  Suggestion: kbox init --name %s", cfg.Metadata.Name, suggested)
}
```

**Pros:**
- User is aware of the issue
- User chooses the final name explicitly
- No surprises

**Cons:**
- Extra friction for users
- Requires user action

### Option C: Prompt user interactively

When an invalid name is detected, prompt the user:

```go
if !IsValidName(cfg.Metadata.Name) {
    suggested := SanitizeName(cfg.Metadata.Name)
    fmt.Printf("Directory name %q contains invalid characters for Kubernetes.\n", cfg.Metadata.Name)
    fmt.Printf("Suggested name: %s\n", suggested)
    fmt.Print("Enter app name (or press Enter to use suggestion): ")

    var input string
    fmt.Scanln(&input)
    if input == "" {
        cfg.Metadata.Name = suggested
    } else {
        cfg.Metadata.Name = input
        if !IsValidName(cfg.Metadata.Name) {
            return fmt.Errorf("name %q is still invalid", cfg.Metadata.Name)
        }
    }
}
```

**Pros:**
- User is aware of the issue
- Provides convenient default
- User has control

**Cons:**
- Breaks non-interactive/scripted usage
- More complex implementation
- Inconsistent with other kbox commands

## Recommended Fix

**Option A (Auto-sanitize) with informational output** is the recommended approach.

**Rationale:**

1. **User experience:** Most users running `kbox init` want to get started quickly. Requiring manual intervention for something that can be automatically fixed adds unnecessary friction.

2. **Predictability:** The sanitization rules are deterministic and documented. Users can predict what their name will become.

3. **Precedent:** Docker Compose, Helm, and other K8s tools auto-sanitize names. Users expect this behavior.

4. **Transparency:** Print a message when sanitization occurs so users are aware:
   ```
   Scanning directory...
     Note: Directory name "my app" sanitized to "my-app" (K8s naming requirements)
   ```

5. **Override available:** Users can always use `--name` flag if they want a specific name:
   ```
   kbox init --name my-custom-name
   ```

**Implementation locations:**
1. Add `SanitizeName()` to `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/validate.go`
2. Update `runInit()` in `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/init.go` to use sanitization
3. Update `InferFromDockerfile()` in `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/loader.go` to use sanitization
4. Add unit tests for `SanitizeName()` covering edge cases:
   - Spaces: `"my app"` -> `"my-app"`
   - Underscores: `"my_app"` -> `"my-app"`
   - Uppercase: `"MyApp"` -> `"myapp"`
   - Leading number: `"123app"` -> `"app-123app"`
   - All invalid: `"___"` -> `"app"`
   - Too long: `64+ chars` -> truncated to 63
