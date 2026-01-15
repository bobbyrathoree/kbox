package render

import (
	"github.com/bobbyrathoree/kbox/internal/config"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RenderJobs creates Job and CronJob resources from the config
func (r *Renderer) RenderJobs() ([]*batchv1.Job, []*batchv1.CronJob, error) {
	var jobs []*batchv1.Job
	var cronJobs []*batchv1.CronJob

	for _, jc := range r.config.Spec.Jobs {
		if jc.Schedule != "" {
			// This is a CronJob
			cronJob := r.renderCronJob(jc)
			cronJobs = append(cronJobs, cronJob)
		} else {
			// This is a regular Job
			job := r.renderJob(jc)
			jobs = append(jobs, job)
		}
	}

	return jobs, cronJobs, nil
}

// renderJob creates a Job resource
func (r *Renderer) renderJob(jc config.JobConfig) *batchv1.Job {
	// Default to app image if not specified
	image := jc.Image
	if image == "" {
		image = r.config.Spec.Image
	}

	// Build container
	container := corev1.Container{
		Name:    jc.Name,
		Image:   image,
		Command: jc.Command,
		Args:    jc.Args,
	}

	// Add env vars if specified
	if len(jc.Env) > 0 {
		for k, v := range jc.Env {
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  k,
				Value: v,
			})
		}
	}

	// Share volume mounts with jobs
	if len(r.config.Spec.Volumes) > 0 {
		container.VolumeMounts = r.renderVolumeMounts()
	}

	// Add container-level security context
	container.SecurityContext = defaultContainerSecurityContext()

	// Default backoff limit
	var backoffLimit int32 = 3
	if jc.BackoffLimit != nil {
		backoffLimit = *jc.BackoffLimit
	}

	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.config.Metadata.Name + "-" + jc.Name,
			Namespace: r.Namespace(),
			Labels:    r.jobLabels(jc.Name),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: r.jobLabels(jc.Name),
				},
				Spec: corev1.PodSpec{
					SecurityContext: defaultPodSecurityContext(),
					RestartPolicy:   corev1.RestartPolicyNever,
					Containers:      []corev1.Container{container},
					Volumes:         r.renderPodVolumes(),
				},
			},
		},
	}

	// Set TTL for automatic cleanup
	if jc.TTLSecondsAfterFinished != nil {
		job.Spec.TTLSecondsAfterFinished = jc.TTLSecondsAfterFinished
	}

	return job
}

// renderCronJob creates a CronJob resource
func (r *Renderer) renderCronJob(jc config.JobConfig) *batchv1.CronJob {
	// Default to app image if not specified
	image := jc.Image
	if image == "" {
		image = r.config.Spec.Image
	}

	// Build container
	container := corev1.Container{
		Name:    jc.Name,
		Image:   image,
		Command: jc.Command,
		Args:    jc.Args,
	}

	// Add env vars if specified
	if len(jc.Env) > 0 {
		for k, v := range jc.Env {
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  k,
				Value: v,
			})
		}
	}

	// Share volume mounts with jobs
	if len(r.config.Spec.Volumes) > 0 {
		container.VolumeMounts = r.renderVolumeMounts()
	}

	// Add container-level security context
	container.SecurityContext = defaultContainerSecurityContext()

	// Default backoff limit
	var backoffLimit int32 = 3
	if jc.BackoffLimit != nil {
		backoffLimit = *jc.BackoffLimit
	}

	cronJob := &batchv1.CronJob{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "CronJob",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.config.Metadata.Name + "-" + jc.Name,
			Namespace: r.Namespace(),
			Labels:    r.jobLabels(jc.Name),
		},
		Spec: batchv1.CronJobSpec{
			Schedule:          jc.Schedule,
			ConcurrencyPolicy: batchv1.ForbidConcurrent,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: r.jobLabels(jc.Name),
				},
				Spec: batchv1.JobSpec{
					BackoffLimit: &backoffLimit,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: r.jobLabels(jc.Name),
						},
						Spec: corev1.PodSpec{
							SecurityContext: defaultPodSecurityContext(),
							RestartPolicy:   corev1.RestartPolicyNever,
							Containers:      []corev1.Container{container},
							Volumes:         r.renderPodVolumes(),
						},
					},
				},
			},
		},
	}

	// Set TTL for automatic cleanup of job instances
	if jc.TTLSecondsAfterFinished != nil {
		cronJob.Spec.JobTemplate.Spec.TTLSecondsAfterFinished = jc.TTLSecondsAfterFinished
	}

	return cronJob
}

// jobLabels returns labels for a job
func (r *Renderer) jobLabels(jobName string) map[string]string {
	labels := r.Labels()
	labels["kbox.dev/job"] = jobName
	return labels
}

// GetPreDeployJobs returns jobs that should run before deployment
func (r *Renderer) GetPreDeployJobs() []config.JobConfig {
	var preDeployJobs []config.JobConfig
	for _, jc := range r.config.Spec.Jobs {
		if jc.RunBefore == "deploy" && jc.Schedule == "" {
			preDeployJobs = append(preDeployJobs, jc)
		}
	}
	return preDeployJobs
}

// RenderSingleJob renders a single job for manual execution
func (r *Renderer) RenderSingleJob(jc config.JobConfig) *batchv1.Job {
	return r.renderJob(jc)
}
