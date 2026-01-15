package debug

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

// ShellOptions configures shell execution
type ShellOptions struct {
	Container string
	Command   []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	TTY       bool
}

// DefaultShellOptions returns sensible defaults for interactive shell
func DefaultShellOptions() ShellOptions {
	return ShellOptions{
		Command: []string{"/bin/sh"},
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		TTY:     true,
	}
}

// ExecResult contains the result of a shell exec
type ExecResult struct {
	UsedEphemeral bool
	EphemeralName string
}

// Shell opens an interactive shell in a pod
// It tries multiple strategies:
// 1. Direct exec with /bin/bash
// 2. Direct exec with /bin/sh
// 3. Ephemeral debug container (for distroless)
func Shell(ctx context.Context, client *kubernetes.Clientset, config *rest.Config, namespace, podName string, opts ShellOptions) (*ExecResult, error) {
	result := &ExecResult{}

	// Get container name if not specified
	containerName := opts.Container
	if containerName == "" {
		var err error
		containerName, err = GetPodContainer(ctx, client, namespace, podName)
		if err != nil {
			return nil, err
		}
	}

	// Try /bin/bash first
	bashOpts := opts
	bashOpts.Command = []string{"/bin/bash"}
	bashOpts.Container = containerName
	err := execInPod(ctx, client, config, namespace, podName, bashOpts)
	if err == nil {
		return result, nil
	}

	// Try /bin/sh
	shOpts := opts
	shOpts.Command = []string{"/bin/sh"}
	shOpts.Container = containerName
	err = execInPod(ctx, client, config, namespace, podName, shOpts)
	if err == nil {
		return result, nil
	}

	// Fall back to ephemeral container
	fmt.Fprintf(opts.Stderr, "No shell available in container, using ephemeral debug container...\n")
	return execEphemeral(ctx, client, config, namespace, podName, containerName, opts)
}

func execInPod(ctx context.Context, client *kubernetes.Clientset, config *rest.Config, namespace, podName string, opts ShellOptions) error {
	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: opts.Container,
			Command:   opts.Command,
			Stdin:     opts.Stdin != nil,
			Stdout:    opts.Stdout != nil,
			Stderr:    opts.Stderr != nil,
			TTY:       opts.TTY,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	streamOpts := remotecommand.StreamOptions{
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
		Tty:    opts.TTY,
	}

	return exec.StreamWithContext(ctx, streamOpts)
}

func execEphemeral(ctx context.Context, client *kubernetes.Clientset, config *rest.Config, namespace, podName, targetContainer string, opts ShellOptions) (*ExecResult, error) {
	result := &ExecResult{UsedEphemeral: true}

	// Generate ephemeral container name
	ephemeralName := fmt.Sprintf("kbox-debug-%d", randomSuffix())
	result.EphemeralName = ephemeralName

	// Get the pod
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	// Create ephemeral container spec
	ec := corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name:  ephemeralName,
			Image: "busybox:1.36.1", // Small debug image
			Command: []string{"/bin/sh"},
			Stdin:           true,
			TTY:             opts.TTY,
			ImagePullPolicy: corev1.PullIfNotPresent,
		},
		TargetContainerName: targetContainer,
	}

	// Add the ephemeral container to the pod
	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, ec)

	_, err = client.CoreV1().Pods(namespace).UpdateEphemeralContainers(ctx, podName, pod, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create ephemeral container (cluster may not support this): %w", err)
	}

	fmt.Fprintf(opts.Stderr, "Created ephemeral container %q, waiting for it to start...\n", ephemeralName)

	// Wait for ephemeral container to be running
	if err := waitForEphemeralContainer(ctx, client, namespace, podName, ephemeralName); err != nil {
		return nil, err
	}

	// Exec into the ephemeral container
	execOpts := opts
	execOpts.Container = ephemeralName
	execOpts.Command = []string{"/bin/sh"}

	err = execInPod(ctx, client, config, namespace, podName, execOpts)
	return result, err
}

func waitForEphemeralContainer(ctx context.Context, client *kubernetes.Clientset, namespace, podName, containerName string) error {
	for i := 0; i < 30; i++ {
		pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for _, cs := range pod.Status.EphemeralContainerStatuses {
			if cs.Name == containerName {
				if cs.State.Running != nil {
					return nil
				}
				if cs.State.Terminated != nil {
					return fmt.Errorf("ephemeral container terminated: %s", cs.State.Terminated.Reason)
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sleepCtx(ctx, 1):
		}
	}

	return fmt.Errorf("timeout waiting for ephemeral container to start")
}

func randomSuffix() int64 {
	// Simple pseudo-random based on time
	return int64(os.Getpid()) + int64(os.Getuid())
}

func sleepCtx(ctx context.Context, seconds int) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		timer := time.NewTimer(time.Duration(seconds) * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
		close(ch)
	}()
	return ch
}
