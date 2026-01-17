package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/template"
)

var templateCmd = &cobra.Command{
	Use:   "template <type> <name>",
	Short: "Generate project scaffold",
	Long: `Generate a project scaffold with kbox.yaml, Dockerfile, and app code.

Template types:
  api       REST API with health check endpoint
  worker    Background job processor
  cron      Scheduled task (CronJob)

Languages:
  go        Go with net/http
  node      Node.js with Express
  python    Python with FastAPI

Examples:
  kbox template api myservice --lang go
  kbox template worker jobs --lang python
  kbox template cron cleanup --lang node`,
	Args: cobra.ExactArgs(2),
	RunE: runTemplate,
}

func init() {
	templateCmd.Flags().StringP("lang", "l", "", "Language/framework (go, node, python)")
	templateCmd.Flags().StringP("dir", "d", "", "Output directory (default: ./<name>)")
	templateCmd.Flags().IntP("port", "p", 8080, "Port number for API services")
	templateCmd.Flags().String("db", "", "Include database (postgres, redis)")
	templateCmd.Flags().BoolP("force", "f", false, "Overwrite existing files")

	rootCmd.AddCommand(templateCmd)
}

func runTemplate(cmd *cobra.Command, args []string) error {
	templateType := args[0]
	name := args[1]

	lang, _ := cmd.Flags().GetString("lang")
	dir, _ := cmd.Flags().GetString("dir")
	port, _ := cmd.Flags().GetInt("port")
	db, _ := cmd.Flags().GetString("db")
	force, _ := cmd.Flags().GetBool("force")

	// Validate template type
	tmplType, ok := template.ValidateType(templateType)
	if !ok {
		types := []string{}
		for _, t := range template.SupportedTypes() {
			types = append(types, string(t))
		}
		return fmt.Errorf("unknown template type %q\n  Supported: %s", templateType, strings.Join(types, ", "))
	}

	// Validate or prompt for language
	if lang == "" {
		// For now, default to Go. In Phase 2 we'll add interactive prompts.
		lang = "go"
		fmt.Fprintf(os.Stderr, "No --lang specified, using: %s\n", lang)
	}

	tmplLang, ok := template.ValidateLanguage(lang)
	if !ok {
		langs := []string{}
		for _, l := range template.SupportedLanguages() {
			langs = append(langs, string(l))
		}
		return fmt.Errorf("unknown language %q\n  Supported: %s", lang, strings.Join(langs, ", "))
	}

	// Validate database if specified
	if db != "" && db != "none" && db != "postgres" && db != "redis" && db != "mongodb" && db != "mysql" {
		return fmt.Errorf("unknown database %q\n  Supported: postgres, redis, mongodb, mysql, none", db)
	}
	if db == "none" {
		db = ""
	}

	// Create generator config
	cfg := template.Config{
		Name:     name,
		OutputDir: dir,
		Type:     tmplType,
		Lang:     tmplLang,
		Port:     port,
		Database: db,
		Force:    force,
	}

	// Generate scaffold
	fmt.Fprintf(os.Stderr, "Creating %s scaffold for %s...\n\n", tmplType, name)

	generator := template.NewGenerator(cfg)
	if err := generator.Generate(); err != nil {
		return err
	}

	// Print next steps
	outputDir := name
	if dir != "" {
		outputDir = dir
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Next steps:")
	fmt.Fprintf(os.Stderr, "  cd %s\n", outputDir)
	fmt.Fprintln(os.Stderr, "  kbox up        # Build and deploy")
	fmt.Fprintln(os.Stderr, "  kbox logs      # View logs")

	return nil
}
