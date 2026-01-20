# Investigation: kbox share Port Bug

## Summary
The `kbox share` command correctly implements port resolution from `kbox.yaml`, but defaults to hardcoded port 8080 when `spec.port` is not set or is 0 in the config file. This is actually expected behavior for fallback, but the bug may occur when the config file explicitly defines a port that is somehow not being read properly.

## Code Location
**File:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/share.go`

**Hardcoded port 8080 appears in three locations within `resolveShareTarget()` function:**

1. **Line 196-197** - When app name is provided as argument but port cannot be resolved from multi-service config:
```go
if port == 0 {
    port = 8080 // Default
}
```

2. **Line 228-230** - When using multi-service config but no port is defined for the first service:
```go
if port == 0 {
    port = 8080
}
```

3. **Line 243-245** - When using single-app config but `cfg.Spec.Port` is 0:
```go
if port == 0 {
    port = 8080
}
```

## Root Cause
The code **does** attempt to read the port from `kbox.yaml`:

- For single-app configs: `port := cfg.Spec.Port` (line 239)
- For multi-service configs: `port := firstSvc.Port` (line 224) or `svc.Port` (line 189)

However, there are two potential issues:

1. **Config loader applies defaults AFTER loading**: The `config.Loader.Load()` method calls `config.WithDefaults()` which sets `Spec.Port = 8080` if it's 0 (see `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/schema.go` lines 440-441). This means single-app configs should always have port 8080 as default.

2. **Multi-service configs do NOT apply defaults**: The `ServiceSpec` type in multi-service configs does not have a `WithDefaults()` equivalent, so `svc.Port` can legitimately be 0 even when loaded.

3. **The --port flag IS being respected**: Line 48 reads `portOverride, _ := cmd.Flags().GetInt("port")` and this value is checked before falling back to 8080 (lines 180-182, 225-227, 240-242).

**The actual bug**: When an app name is passed as an argument (e.g., `kbox share myapp`), the code only checks multi-service config for the port (lines 184-193). If the config is actually a single-app config with a different port, that port is never read - it falls through to the default 8080.

## Expected Behavior
The port should be determined in this priority order:
1. `--port` flag if provided
2. Port from `kbox.yaml` (either `spec.port` for single-app or `services.<name>.port` for multi-service)
3. Default 8080 as last resort

## Proposed Fix
In `resolveShareTarget()`, when an app name is provided as argument, also check single-app config:

```go
// In resolveShareTarget, around lines 178-199, modify to:
if len(args) > 0 {
    appName := args[0]
    port := portOverride

    if port == 0 {
        // Try to get port from multi-service config first
        isMulti, _ := loader.IsMultiService()
        if isMulti {
            cfg, err := loader.LoadMultiService()
            if err == nil {
                if svc, ok := cfg.Services[appName]; ok && svc.Port > 0 {
                    port = svc.Port
                }
            }
        } else {
            // Try single-app config
            cfg, err := loader.Load()
            if err == nil && cfg.Metadata.Name == appName && cfg.Spec.Port > 0 {
                port = cfg.Spec.Port
            }
        }
    }

    if port == 0 {
        port = 8080 // Default
    }

    return appName, port, nil
}
```

Additionally, consider using the constant `config.DefaultPort` instead of hardcoded `8080` for consistency:

```go
import "github.com/bobbyrathoree/kbox/internal/config"
// ...
if port == 0 {
    port = config.DefaultPort
}
```

This ensures consistency with the default defined in `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/config/schema.go` line 413.
