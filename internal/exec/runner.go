package exec

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// Options configures the exec runner
type Options struct {
	Image       string        // Image to use (required)
	Command     string        // Command to run
	Namespace   string        // Namespace to create pod in
	AppName     string        // App name for labeling
	Interactive bool          // Keep stdin open
	TTY         bool          // Allocate TTY
	Timeout     time.Duration // Timeout for execution
	EnvVars     map[string]string // Environment variables
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
}

// Result contains the execution result
type Result struct {
	ExitCode int
	PodName  string
}

// Runner executes one-off commands in temporary pods
type Runner struct {
	client *kubernetes.Clientset
	config *rest.Config
}

// NewRunner creates a new exec runner
func NewRunner(client *kubernetes.Clientset, config *rest.Config) *Runner {
	return &Runner{
		client: client,
		config: config,
	}
}

// Run creates a temporary pod, runs the command, and cleans up
func (r *Runner) Run(ctx context.Context, opts Options) (*Result, error) {
	result := &Result{}

	// Set defaults
	if opts.Timeout == 0 {
		opts.Timeout = 10 * time.Minute
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	// Generate unique pod name
	podName := fmt.Sprintf("exec-%s-%d", opts.AppName, time.Now().Unix())
	result.PodName = podName

	// Build pod spec
	pod := r.buildPod(podName, opts)

	// Create the pod
	fmt.Fprintf(opts.Stderr, "Creating pod %s...\n", podName)
	_, err := r.client.CoreV1().Pods(opts.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return result, fmt.Errorf("failed to create pod: %w", err)
	}

	// Ensure cleanup
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		fmt.Fprintf(opts.Stderr, "Cleaning up...\n")
		_ = r.client.CoreV1().Pods(opts.Namespace).Delete(cleanupCtx, podName, metav1.DeleteOptions{})
	}()

	// Wait for pod to be running
	fmt.Fprintf(opts.Stderr, "Waiting for pod to start...\n")
	if err := r.waitForPodRunning(ctx, opts.Namespace, podName); err != nil {
		return result, err
	}

	fmt.Fprintln(opts.Stderr)

	// For interactive mode, attach to the pod
	if opts.Interactive || opts.TTY {
		err = r.attachToPod(ctx, opts.Namespace, podName, opts)
	} else {
		// For non-interactive, stream logs
		err = r.streamLogs(ctx, opts.Namespace, podName, opts.Stdout)
	}

	if err != nil {
		return result, err
	}

	// Wait for pod completion and get exit code
	exitCode, err := r.waitForPodCompletion(ctx, opts.Namespace, podName)
	result.ExitCode = exitCode

	if err != nil {
		return result, err
	}

	fmt.Fprintln(opts.Stderr)
	if exitCode == 0 {
		fmt.Fprintf(opts.Stderr, "Pod completed successfully (exit code 0)\n")
	} else {
		fmt.Fprintf(opts.Stderr, "Pod failed (exit code %d)\n", exitCode)
	}

	return result, nil
}

func (r *Runner) buildPod(name string, opts Options) *corev1.Pod {
	// Build env vars
	var envVars []corev1.EnvVar
	for k, v := range opts.EnvVars {
		envVars = append(envVars, corev1.EnvVar{Name: k, Value: v})
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: opts.Namespace,
			Labels: map[string]string{
				"kbox.dev/exec": "true",
				"kbox.dev/app":  opts.AppName,
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "exec",
					Image:   opts.Image,
					Command: []string{"/bin/sh", "-c"},
					Args:    []string{opts.Command},
					Stdin:   opts.Interactive,
					TTY:     opts.TTY,
					Env:     envVars,
				},
			},
		},
	}

	return pod
}

func (r *Runner) waitForPodRunning(ctx context.Context, namespace, name string) error {
	for {
		pod, err := r.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get pod status: %w", err)
		}

		switch pod.Status.Phase {
		case corev1.PodRunning:
			return nil
		case corev1.PodSucceeded, corev1.PodFailed:
			// Pod already finished (fast command)
			return nil
		case corev1.PodPending:
			// Check for container errors
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil && cs.State.Waiting.Reason == "ErrImagePull" {
					return fmt.Errorf("failed to pull image: %s", cs.State.Waiting.Message)
				}
				if cs.State.Waiting != nil && cs.State.Waiting.Reason == "ImagePullBackOff" {
					return fmt.Errorf("failed to pull image: %s", cs.State.Waiting.Message)
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}

func (r *Runner) waitForPodCompletion(ctx context.Context, namespace, name string) (int, error) {
	for {
		pod, err := r.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return -1, fmt.Errorf("failed to get pod status: %w", err)
		}

		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			return 0, nil
		case corev1.PodFailed:
			// Get exit code from container status
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.Name == "exec" && cs.State.Terminated != nil {
					return int(cs.State.Terminated.ExitCode), nil
				}
			}
			return 1, nil
		}

		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}

func (r *Runner) streamLogs(ctx context.Context, namespace, name string, out io.Writer) error {
	req := r.client.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{
		Container: "exec",
		Follow:    true,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("failed to stream logs: %w", err)
	}
	defer stream.Close()

	_, err = io.Copy(out, stream)
	if err != nil && err != io.EOF {
		return fmt.Errorf("error reading logs: %w", err)
	}

	return nil
}

func (r *Runner) attachToPod(ctx context.Context, namespace, name string, opts Options) error {
	req := r.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(name).
		Namespace(namespace).
		SubResource("attach").
		VersionedParams(&corev1.PodAttachOptions{
			Container: "exec",
			Stdin:     opts.Interactive,
			Stdout:    true,
			Stderr:    true,
			TTY:       opts.TTY,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(r.config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	streamOpts := remotecommand.StreamOptions{
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
		Tty:    opts.TTY,
	}

	if opts.Interactive {
		streamOpts.Stdin = opts.Stdin
	}

	return exec.StreamWithContext(ctx, streamOpts)
}
