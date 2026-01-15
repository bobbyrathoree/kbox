package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/render"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage jobs",
	Long: `Manage one-off and scheduled jobs.

Jobs are tasks that run to completion (unlike deployments that run continuously).
Use jobs for migrations, data processing, cleanup tasks, etc.

Examples:
  kbox job run migrate         # Run the 'migrate' job
  kbox job list                # List all jobs and their status
  kbox job logs migrate        # View logs from the last 'migrate' job run`,
}

var jobRunCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Run a one-off job",
	Long: `Run a job defined in your kbox.yaml.

This creates a new Job resource in Kubernetes and waits for it to complete.
The job must be defined in the 'jobs' section of your kbox.yaml.`,
	Example: `  # Run the migrate job
  kbox job run migrate

  # Run with verbose output
  kbox job run migrate -v`,
	Args: cobra.ExactArgs(1),
	RunE: runJobRun,
}

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List jobs and their status",
	Long:  `List all jobs defined in kbox.yaml and their current status in the cluster.`,
	Example: `  kbox job list
  kbox job list --output=json`,
	RunE: runJobList,
}

var jobLogsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "View job logs",
	Long:  `View logs from the most recent run of a job.`,
	Example: `  # View logs from the migrate job
  kbox job logs migrate

  # Follow logs
  kbox job logs migrate -f`,
	Args: cobra.ExactArgs(1),
	RunE: runJobLogs,
}

func runJobRun(cmd *cobra.Command, args []string) error {
	jobName := args[0]
	kubeContext, _ := cmd.Flags().GetString("context")
	namespace, _ := cmd.Flags().GetString("namespace")
	ciMode := IsCIMode(cmd)
	outputFormat := GetOutputFormat(cmd)

	// Load config
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	// Find the job in config
	var jobConfig *config.JobConfig
	for i := range cfg.Spec.Jobs {
		if cfg.Spec.Jobs[i].Name == jobName {
			jobConfig = &cfg.Spec.Jobs[i]
			break
		}
	}

	if jobConfig == nil {
		return fmt.Errorf("job %q not found in kbox.yaml\n  → Available jobs: %s", jobName, listJobNames(cfg.Spec.Jobs))
	}

	if jobConfig.Schedule != "" {
		return fmt.Errorf("job %q is a CronJob and cannot be run manually\n  → CronJobs run on their schedule: %s", jobName, jobConfig.Schedule)
	}

	// Connect to cluster
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context: kubeContext,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// Determine namespace
	if namespace == "" {
		namespace = cfg.Metadata.Namespace
		if namespace == "" {
			namespace = "default"
		}
	}

	// Render the job
	renderer := render.New(cfg)
	job := renderer.RenderSingleJob(*jobConfig)
	job.Namespace = namespace

	// Add unique suffix for this run
	runID := fmt.Sprintf("%d", time.Now().Unix())
	job.Name = fmt.Sprintf("%s-%s-%s", cfg.Metadata.Name, jobName, runID)

	if !ciMode {
		fmt.Printf("Running job %q...\n", jobName)
	}

	// Create the job
	createdJob, err := client.Clientset.BatchV1().Jobs(namespace).Create(cmd.Context(), job, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	if !ciMode {
		fmt.Printf("  Created Job/%s\n", createdJob.Name)
		fmt.Println("  Waiting for completion...")
	}

	// Wait for job completion
	completed, err := waitForJob(cmd.Context(), client, namespace, createdJob.Name, ciMode)
	if err != nil {
		return err
	}

	// Output
	if outputFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"success":   completed.Status.Succeeded > 0,
			"name":      createdJob.Name,
			"namespace": namespace,
			"status":    getJobStatus(completed),
		})
	}

	if !ciMode {
		if completed.Status.Succeeded > 0 {
			fmt.Printf("  Job completed successfully\n")
		} else {
			fmt.Printf("  Job failed\n")
			fmt.Printf("  → Run 'kbox job logs %s' to see output\n", jobName)
		}
	}

	if completed.Status.Failed > 0 {
		return fmt.Errorf("job failed")
	}

	return nil
}

func runJobList(cmd *cobra.Command, args []string) error {
	kubeContext, _ := cmd.Flags().GetString("context")
	namespace, _ := cmd.Flags().GetString("namespace")
	ciMode := IsCIMode(cmd)
	outputFormat := GetOutputFormat(cmd)

	// Load config
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	if len(cfg.Spec.Jobs) == 0 {
		if !ciMode {
			fmt.Println("No jobs defined in kbox.yaml")
			fmt.Println("  → Add a 'jobs' section to your kbox.yaml to define jobs")
		}
		return nil
	}

	// Connect to cluster
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context: kubeContext,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// Determine namespace
	if namespace == "" {
		namespace = cfg.Metadata.Namespace
		if namespace == "" {
			namespace = "default"
		}
	}

	// Get jobs and cronjobs from cluster
	jobs, _ := client.Clientset.BatchV1().Jobs(namespace).List(cmd.Context(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", cfg.Metadata.Name),
	})

	cronJobs, _ := client.Clientset.BatchV1().CronJobs(namespace).List(cmd.Context(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", cfg.Metadata.Name),
	})

	type jobInfo struct {
		Name       string `json:"name"`
		Type       string `json:"type"`
		Schedule   string `json:"schedule,omitempty"`
		LastRun    string `json:"lastRun,omitempty"`
		Status     string `json:"status"`
		InCluster  bool   `json:"inCluster"`
	}

	var jobInfos []jobInfo

	for _, jc := range cfg.Spec.Jobs {
		info := jobInfo{
			Name: jc.Name,
		}

		if jc.Schedule != "" {
			info.Type = "CronJob"
			info.Schedule = jc.Schedule
			// Find in cluster
			for _, cj := range cronJobs.Items {
				if cj.Labels["kbox.dev/job"] == jc.Name {
					info.InCluster = true
					if cj.Status.LastScheduleTime != nil {
						info.LastRun = formatAge(cj.Status.LastScheduleTime.Time)
					}
					info.Status = "Scheduled"
					break
				}
			}
		} else {
			info.Type = "Job"
			// Find most recent job run in cluster
			for _, j := range jobs.Items {
				if j.Labels["kbox.dev/job"] == jc.Name {
					info.InCluster = true
					info.LastRun = formatAge(j.CreationTimestamp.Time)
					info.Status = getJobStatus(&j)
					break
				}
			}
		}

		if !info.InCluster {
			info.Status = "Not deployed"
		}

		jobInfos = append(jobInfos, info)
	}

	// Output
	if outputFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(jobInfos)
	}

	// Table output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tSCHEDULE\tLAST RUN\tSTATUS")
	for _, info := range jobInfos {
		schedule := info.Schedule
		if schedule == "" {
			schedule = "-"
		}
		lastRun := info.LastRun
		if lastRun == "" {
			lastRun = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", info.Name, info.Type, schedule, lastRun, info.Status)
	}
	w.Flush()

	return nil
}

func runJobLogs(cmd *cobra.Command, args []string) error {
	jobName := args[0]
	kubeContext, _ := cmd.Flags().GetString("context")
	namespace, _ := cmd.Flags().GetString("namespace")
	follow, _ := cmd.Flags().GetBool("follow")
	outputFormat := GetOutputFormat(cmd)

	// Load config
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	// Connect to cluster
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context: kubeContext,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// Determine namespace
	if namespace == "" {
		namespace = cfg.Metadata.Namespace
		if namespace == "" {
			namespace = "default"
		}
	}

	// Find the most recent job run
	jobs, err := client.Clientset.BatchV1().Jobs(namespace).List(cmd.Context(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s,kbox.dev/job=%s", cfg.Metadata.Name, jobName),
	})
	if err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	if len(jobs.Items) == 0 {
		return fmt.Errorf("no runs found for job %q\n  → Run 'kbox job run %s' to create one", jobName, jobName)
	}

	// Get the most recent job
	var mostRecent *batchv1.Job
	for i := range jobs.Items {
		if mostRecent == nil || jobs.Items[i].CreationTimestamp.After(mostRecent.CreationTimestamp.Time) {
			mostRecent = &jobs.Items[i]
		}
	}

	// Find pods for this job
	pods, err := client.Clientset.CoreV1().Pods(namespace).List(cmd.Context(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", mostRecent.Name),
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods found for job %q", mostRecent.Name)
	}

	// Get logs from the pod
	pod := pods.Items[0]
	req := client.Clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Follow: follow,
	})

	stream, err := req.Stream(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to get logs: %w", err)
	}
	defer stream.Close()

	// Stream logs to stdout (or JSON)
	if outputFormat == "json" {
		var logs []byte
		buf := make([]byte, 4096)
		for {
			n, err := stream.Read(buf)
			if n > 0 {
				logs = append(logs, buf[:n]...)
			}
			if err != nil {
				break
			}
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"success": true,
			"job":     jobName,
			"pod":     pod.Name,
			"logs":    string(logs),
		})
	}

	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			os.Stdout.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}

	return nil
}

// waitForJob waits for a job to complete
func waitForJob(ctx context.Context, client *k8s.Client, namespace, name string, ciMode bool) (*batchv1.Job, error) {
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for job to complete\n  → Run 'kbox job logs %s' to check status", name)
		case <-ticker.C:
			job, err := client.Clientset.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				continue
			}

			// Check if job is complete
			for _, c := range job.Status.Conditions {
				if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
					return job, nil
				}
				if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
					return job, nil
				}
			}

			if !ciMode {
				fmt.Printf("\r  Waiting... (active: %d, succeeded: %d, failed: %d)",
					job.Status.Active, job.Status.Succeeded, job.Status.Failed)
			}
		}
	}
}

// getJobStatus returns a human-readable status for a job
func getJobStatus(job *batchv1.Job) string {
	if job.Status.Succeeded > 0 {
		return "Completed"
	}
	if job.Status.Failed > 0 {
		return "Failed"
	}
	if job.Status.Active > 0 {
		return "Running"
	}
	return "Pending"
}

// listJobNames returns a comma-separated list of job names
func listJobNames(jobs []config.JobConfig) string {
	if len(jobs) == 0 {
		return "(none)"
	}
	names := ""
	for i, j := range jobs {
		if i > 0 {
			names += ", "
		}
		names += j.Name
	}
	return names
}

func init() {
	// Job logs flags
	jobLogsCmd.Flags().BoolP("follow", "f", false, "Follow log output")

	// Add subcommands
	jobCmd.AddCommand(jobRunCmd)
	jobCmd.AddCommand(jobListCmd)
	jobCmd.AddCommand(jobLogsCmd)

	rootCmd.AddCommand(jobCmd)
}
