# Investigation: Minor Bugs

## Bug 1: kbox down -f flag

- **Code location:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/down.go:43`
- **Issue:** The `down` command creates a loader with hardcoded `"."` as the working directory and doesn't accept a `-f` flag to specify a custom config file. Other commands like `deploy`, `render`, and `validate` accept `-f` flag.

  In `down.go` line 43:
  ```go
  loader := config.NewLoader(".")
  ```

  Compare to `deploy.go` which properly handles `-f` flag:
  ```go
  deployCmd.Flags().StringP("file", "f", "", "Path to kbox.yaml (default: ./kbox.yaml)")
  ```

- **Fix:**
  1. Add `-f` flag to `downCmd` in the `init()` function (line 440-442):
     ```go
     downCmd.Flags().StringP("file", "f", "", "Path to kbox.yaml (default: ./kbox.yaml)")
     ```
  2. In `runDown()`, get the config file flag and pass it to the loader:
     ```go
     configFile, _ := cmd.Flags().GetString("file")
     // If specific file provided, load it directly
     if configFile != "" {
         cfg, err := loader.LoadFile(configFile)
         // ... use cfg
     }
     ```

---

## Bug 2: kbox diff empty image

- **Code location:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/diff.go:55-66`
- **Issue:** When a config has `build:` but no `image:`, the diff command doesn't handle build-only configs the same way `deploy.go` and `render.go` do. It doesn't set a placeholder image, leading to comparing `existingImage -> ""`.

  In `diff.go` lines 55-66, config is loaded and environment overlay applied, but there's no handling for build-only configs:
  ```go
  if environment != "" {
      cfg = cfg.ForEnvironment(environment)
  }
  cfg.WithDefaults()
  // Missing: check for build-only config and set placeholder image
  ```

  Compare to `deploy.go` lines 162-164:
  ```go
  if cfg.Spec.Image == "" && cfg.Spec.Build != nil {
      cfg.Spec.Image = fmt.Sprintf("%s:latest", cfg.Metadata.Name)
  }
  ```

- **Fix:** Add the same placeholder image logic in `diff.go` after `cfg.WithDefaults()`:
  ```go
  // If only build config, use a placeholder image (same as deploy)
  if cfg.Spec.Image == "" && cfg.Spec.Build != nil {
      cfg.Spec.Image = fmt.Sprintf("%s:latest", cfg.Metadata.Name)
  }
  ```

---

## Bug 3: --summary undercounts with Jobs

- **Code location:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/cli/render.go:165-206`
- **Issue:** The `printBundleSummary()` function counts total resources using `bundle.AllObjects()` which includes Jobs and CronJobs, but the individual breakdown in the summary doesn't list Jobs or CronJobs. This makes the total count appear incorrect.

  In `render.go` line 166:
  ```go
  total := len(bundle.AllObjects())  // Includes Jobs, CronJobs
  ```

  But the function only lists: Deployment, Services, Secrets, ServiceAccount, HPA, PDB, NetworkPolicies, StatefulSets. It's missing:
  - Jobs
  - CronJobs
  - ConfigMaps
  - Ingresses
  - ServiceMonitors

- **Fix:** Add the missing resource types to `printBundleSummary()`:
  ```go
  // Add after Secrets section:
  if len(bundle.ConfigMaps) > 0 {
      fmt.Printf("  ConfigMaps:      %d\n", len(bundle.ConfigMaps))
  }
  if len(bundle.Jobs) > 0 {
      fmt.Printf("  Jobs:            %d\n", len(bundle.Jobs))
      for _, job := range bundle.Jobs {
          fmt.Printf("    - %s\n", job.Name)
      }
  }
  if len(bundle.CronJobs) > 0 {
      fmt.Printf("  CronJobs:        %d\n", len(bundle.CronJobs))
      for _, cj := range bundle.CronJobs {
          fmt.Printf("    - %s (schedule: %s)\n", cj.Name, cj.Spec.Schedule)
      }
  }
  if len(bundle.Ingresses) > 0 {
      fmt.Printf("  Ingresses:       %d\n", len(bundle.Ingresses))
  }
  ```

---

## Bug 4: render doesn't resolve relative paths

- **Code location:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/secrets/envfile.go:21-22`
- **Issue:** When a user specifies a relative path like `secrets.fromEnvFile: ".env"` in `kbox.yaml`, the path is passed directly to `os.Open()` without being resolved relative to the config file's directory. This works when running from the same directory as `kbox.yaml`, but breaks when using `-f` with a config in another directory.

  In `envfile.go` line 21-22:
  ```go
  func LoadEnvFile(path string) (map[string]string, error) {
      file, err := os.Open(path)  // path is not resolved relative to config file
  ```

  The path comes from `render.go` line 260:
  ```go
  envFile := r.config.Spec.Secrets.FromEnvFile  // e.g., ".env"
  ```

- **Fix:** The renderer needs to resolve the path relative to the config file's directory. Two options:

  **Option A (Preferred):** Store the config file's directory in the config or renderer:
  ```go
  // In render/render.go RenderSecretFromEnvFile():
  envFile := r.config.Spec.Secrets.FromEnvFile
  if !filepath.IsAbs(envFile) {
      envFile = filepath.Join(r.configDir, envFile)
  }
  ```

  **Option B:** Resolve in the loader when parsing the config file, converting relative paths to absolute before storing.

---

## Bug 5: kbox db connect confusing errors

- **Code location:** `/Users/bobbyrathore/Documents/WildProjects/kbox/internal/database/find.go:51-52`
- **Issue:** When no database pods exist, the error message suggests using `kbox add <type>` but doesn't explain that the database might not be deployed yet or might be in a different namespace.

  In `find.go` lines 51-52:
  ```go
  if len(ssList.Items) == 0 {
      return nil, fmt.Errorf("no %s database found\n  Add one with: kbox add %s", dbType, dbType)
  }
  ```

  This error is misleading when:
  - The database is configured in `kbox.yaml` but not deployed yet
  - The database exists in a different namespace
  - The StatefulSet exists but pod isn't running

- **Fix:** Improve the error message to be more helpful:
  ```go
  if len(ssList.Items) == 0 {
      return nil, fmt.Errorf("no %s database found in namespace %q\n"+
          "  Possible causes:\n"+
          "  - Database not deployed yet: run 'kbox deploy'\n"+
          "  - Wrong namespace: use '-n <namespace>' flag\n"+
          "  - Database not configured: add to kbox.yaml with 'kbox add %s'",
          dbType, namespace, dbType)
  }
  ```

---

## Priority Order

Based on impact (how often users encounter it) and effort (how easy to fix):

1. **Bug 1: kbox down -f flag** (High Impact, Low Effort)
   - Users working with multiple config files are completely blocked
   - Simple flag addition, mirrors existing deploy/validate/render pattern

2. **Bug 2: kbox diff empty image** (Medium Impact, Low Effort)
   - Affects all users with build-only configs
   - One-line fix copying existing pattern

3. **Bug 5: kbox db connect errors** (Medium Impact, Low Effort)
   - Confusing UX causes support overhead
   - Simple string change with no logic impact

4. **Bug 3: --summary undercounts** (Low Impact, Low Effort)
   - Cosmetic issue, summary is informational
   - Easy to add missing resource types

5. **Bug 4: render relative paths** (Low Impact, Medium Effort)
   - Only affects users using -f with configs in other directories
   - Requires passing config directory through the render pipeline
