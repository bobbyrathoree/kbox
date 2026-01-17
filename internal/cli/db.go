package cli

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/bobbyrathoree/kbox/internal/database"
	"github.com/bobbyrathoree/kbox/internal/dependencies"
	"github.com/bobbyrathoree/kbox/internal/k8s"
)

var dbCmd = &cobra.Command{
	Use:   "db <subcommand>",
	Short: "Database operations",
	Long: `Manage database dependencies.

Connect to databases, create backups, and restore data.

Subcommands:
  connect    Open interactive database shell
  backup     Create database backup
  restore    Restore from backup file
  exec       Run one-off database command

Examples:
  kbox db connect postgres           # Connect to postgres
  kbox db connect myapp-postgres     # Connect by service name
  kbox db backup postgres            # Backup postgres
  kbox db restore postgres -i dump.sql`,
}

var dbConnectCmd = &cobra.Command{
	Use:   "connect <type|service-name>",
	Short: "Connect to database",
	Long: `Open an interactive shell to a database.

Specify either a database type (postgres, redis, mongodb, mysql) or
a specific service name. If multiple databases of the same type exist,
you'll need to specify the service name.

Examples:
  kbox db connect postgres           # Connect to postgres
  kbox db connect myapp-postgres     # Connect by service name
  kbox db connect redis              # Connect to redis`,
	Args: cobra.ExactArgs(1),
	RunE: runDBConnect,
}

var dbBackupCmd = &cobra.Command{
	Use:   "backup <type|service-name>",
	Short: "Backup database",
	Long: `Create a backup of a database.

The backup is saved to a local gzipped file. By default, the filename
includes the service name and timestamp.

Examples:
  kbox db backup postgres                    # Backup to auto-named file
  kbox db backup postgres -o mybackup.sql    # Backup to specific file`,
	Args: cobra.ExactArgs(1),
	RunE: runDBBackup,
}

var dbRestoreCmd = &cobra.Command{
	Use:   "restore <type|service-name>",
	Short: "Restore database",
	Long: `Restore a database from a backup file.

Supports both plain SQL and gzipped files (.gz extension).

Examples:
  kbox db restore postgres -i backup.sql.gz
  kbox db restore postgres -i backup.sql`,
	Args: cobra.ExactArgs(1),
	RunE: runDBRestore,
}

var dbExecCmd = &cobra.Command{
	Use:   "exec <type|service-name> <command>",
	Short: "Execute database command",
	Long: `Run a one-off command in the database.

Examples:
  kbox db exec postgres "SELECT count(*) FROM users"
  kbox db exec redis "KEYS *"`,
	Args: cobra.MinimumNArgs(2),
	RunE: runDBExec,
}

func init() {
	// Backup flags
	dbBackupCmd.Flags().StringP("output", "o", "", "Output file (default: <service>-<timestamp>.sql.gz)")

	// Restore flags
	dbRestoreCmd.Flags().StringP("input", "i", "", "Input file to restore from")
	dbRestoreCmd.MarkFlagRequired("input")

	// Add subcommands
	dbCmd.AddCommand(dbConnectCmd)
	dbCmd.AddCommand(dbBackupCmd)
	dbCmd.AddCommand(dbRestoreCmd)
	dbCmd.AddCommand(dbExecCmd)

	rootCmd.AddCommand(dbCmd)
}

func runDBConnect(cmd *cobra.Command, args []string) error {
	dbSpec := args[0]
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	// Create K8s client
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	ns := client.Namespace
	if namespace != "" {
		ns = namespace
	}

	// Find the database
	db, err := database.ResolveDatabase(cmd.Context(), client.Clientset, ns, dbSpec)
	if err != nil {
		return err
	}

	// Connect
	opts := database.DefaultConnectOptions()
	return database.Connect(cmd.Context(), client.Clientset, client.RestConfig, db, opts)
}

func runDBBackup(cmd *cobra.Command, args []string) error {
	dbSpec := args[0]
	output, _ := cmd.Flags().GetString("output")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	// Create K8s client
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	ns := client.Namespace
	if namespace != "" {
		ns = namespace
	}

	// Find the database
	db, err := database.ResolveDatabase(cmd.Context(), client.Clientset, ns, dbSpec)
	if err != nil {
		return err
	}

	// Get backup command
	template, ok := dependencies.Get(db.Type)
	if !ok {
		return fmt.Errorf("unsupported database type: %s", db.Type)
	}

	if len(template.BackupCommand) == 0 {
		return fmt.Errorf("backup not supported for %s", db.Type)
	}

	// Generate output filename if not specified
	if output == "" {
		timestamp := time.Now().Format("2006-01-02-150405")
		output = fmt.Sprintf("%s-%s.sql.gz", db.ServiceName, timestamp)
	}

	// Ensure .gz extension
	if !strings.HasSuffix(output, ".gz") {
		output += ".gz"
	}

	fmt.Fprintf(os.Stderr, "Backing up %s...\n", db.ServiceName)
	fmt.Fprintf(os.Stderr, "  Executing %s in pod...\n", template.BackupCommand[0])

	// Create output file
	outFile, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	// Create gzip writer
	gzWriter := gzip.NewWriter(outFile)
	defer gzWriter.Close()

	// Execute backup command in pod
	req := client.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(db.PodName).
		Namespace(ns).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: template.BackupCommand,
			Stdout:  true,
			Stderr:  true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(client.RestConfig, "POST", req.URL())
	if err != nil {
		os.Remove(output)
		return fmt.Errorf("failed to create executor: %w", err)
	}

	err = exec.StreamWithContext(cmd.Context(), remotecommand.StreamOptions{
		Stdout: gzWriter,
		Stderr: os.Stderr,
	})
	if err != nil {
		os.Remove(output)
		return fmt.Errorf("backup failed: %w", err)
	}

	// Close gzip writer to flush
	gzWriter.Close()

	// Get file size
	info, _ := os.Stat(output)
	size := "unknown"
	if info != nil {
		size = formatSize(info.Size())
	}

	fmt.Fprintf(os.Stderr, "\nSaved: %s (%s)\n", output, size)
	return nil
}

func runDBRestore(cmd *cobra.Command, args []string) error {
	dbSpec := args[0]
	input, _ := cmd.Flags().GetString("input")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	// Validate input file
	if _, err := os.Stat(input); os.IsNotExist(err) {
		return fmt.Errorf("input file not found: %s", input)
	}

	// Create K8s client
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	ns := client.Namespace
	if namespace != "" {
		ns = namespace
	}

	// Find the database
	db, err := database.ResolveDatabase(cmd.Context(), client.Clientset, ns, dbSpec)
	if err != nil {
		return err
	}

	// Get restore command
	template, ok := dependencies.Get(db.Type)
	if !ok {
		return fmt.Errorf("unsupported database type: %s", db.Type)
	}

	if len(template.RestoreCommand) == 0 {
		return fmt.Errorf("restore not supported for %s", db.Type)
	}

	// Open input file
	inFile, err := os.Open(input)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer inFile.Close()

	// Check if gzipped
	var reader io.Reader = inFile
	if strings.HasSuffix(input, ".gz") {
		gzReader, err := gzip.NewReader(inFile)
		if err != nil {
			return fmt.Errorf("failed to read gzip file: %w", err)
		}
		defer gzReader.Close()
		reader = gzReader
	}

	fmt.Fprintf(os.Stderr, "Restoring to %s...\n", db.ServiceName)
	fmt.Fprintf(os.Stderr, "  WARNING: This will overwrite existing data!\n")

	// Execute restore command in pod
	req := client.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(db.PodName).
		Namespace(ns).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: template.RestoreCommand,
			Stdin:   true,
			Stdout:  true,
			Stderr:  true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(client.RestConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	err = exec.StreamWithContext(cmd.Context(), remotecommand.StreamOptions{
		Stdin:  reader,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nRestore complete\n")
	return nil
}

func runDBExec(cmd *cobra.Command, args []string) error {
	dbSpec := args[0]
	command := strings.Join(args[1:], " ")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	// Create K8s client
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	ns := client.Namespace
	if namespace != "" {
		ns = namespace
	}

	// Find the database
	db, err := database.ResolveDatabase(cmd.Context(), client.Clientset, ns, dbSpec)
	if err != nil {
		return err
	}

	// Get connect command and add the user command
	template, ok := dependencies.Get(db.Type)
	if !ok {
		return fmt.Errorf("unsupported database type: %s", db.Type)
	}

	// Build exec command based on database type
	var execCmd []string
	switch db.Type {
	case "postgres":
		execCmd = []string{"psql", "-U", "postgres", "-c", command}
	case "mysql":
		execCmd = []string{"mysql", "-u", "root", "-p$(MYSQL_ROOT_PASSWORD)", "-e", command}
	case "redis":
		execCmd = append(template.ConnectCommand, strings.Split(command, " ")...)
	case "mongodb":
		execCmd = []string{"mongosh", "-u", "root", "-p", "$(MONGO_INITDB_ROOT_PASSWORD)", "--eval", command}
	default:
		return fmt.Errorf("exec not supported for %s", db.Type)
	}

	// Execute command in pod
	req := client.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(db.PodName).
		Namespace(ns).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: execCmd,
			Stdout:  true,
			Stderr:  true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(client.RestConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	return exec.StreamWithContext(cmd.Context(), remotecommand.StreamOptions{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
}

// formatSize formats bytes to human readable string
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Ensure all imports are used
var _ = context.Background
var _ = filepath.Clean
