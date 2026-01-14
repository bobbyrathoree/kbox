package debug

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestPodInfoExtraction tests converting K8s pods to our PodInfo
func TestPodInfoExtraction(t *testing.T) {
	t.Run("extracts basic pod info", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "myapp-abc123",
				Namespace: "default",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "myapp"},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:         "myapp",
						Ready:        true,
						RestartCount: 0,
					},
				},
			},
		}

		info := podToPodInfo(pod)

		if info.Name != "myapp-abc123" {
			t.Errorf("expected name myapp-abc123, got %q", info.Name)
		}
		if info.ContainerName != "myapp" {
			t.Errorf("expected container myapp, got %q", info.ContainerName)
		}
		if !info.Ready {
			t.Error("expected pod to be ready")
		}
		if info.Restarts != 0 {
			t.Errorf("expected 0 restarts, got %d", info.Restarts)
		}
	})

	t.Run("detects restarts", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "crashy-pod"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "app"}},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:         "app",
						Ready:        false,
						RestartCount: 5,
					},
				},
			},
		}

		info := podToPodInfo(pod)

		if info.Ready {
			t.Error("pod should not be ready")
		}
		if info.Restarts != 5 {
			t.Errorf("expected 5 restarts, got %d", info.Restarts)
		}
	})
}

// TestShellFallbackLogic tests the distroless detection concepts
func TestShellFallbackLogic(t *testing.T) {
	t.Run("default shell options are interactive", func(t *testing.T) {
		opts := DefaultShellOptions()

		if !opts.TTY {
			t.Error("default should have TTY enabled")
		}
		if opts.Stdin == nil {
			t.Error("default should have stdin")
		}
		if len(opts.Command) == 0 || opts.Command[0] != "/bin/sh" {
			t.Error("default command should be /bin/sh")
		}
	})

	// Note: Actual ephemeral container testing requires a real cluster
	// This tests the data structures and options are correct
}
