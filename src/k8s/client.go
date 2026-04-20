// Package k8s provides Kubernetes client and manifest generation for package building.
package k8s

import (
	"context"
	"fmt"
	"io"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client wraps Kubernetes client for package build operations.
type Client struct {
	clientset *kubernetes.Clientset
	namespace string
}

// NewClient creates a new Kubernetes client.
// Tries in-cluster config first, falls back to kubeconfig.
func NewClient(kubeconfig, namespace string) (*Client, error) {
	var config *rest.Config
	var err error

	// Try in-cluster config first
	config, err = rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		if kubeconfig == "" {
			kubeconfig = clientcmd.RecommendedHomeFile
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("build kubeconfig: %w", err)
		}
	}

	if namespace == "" {
		namespace = "default"
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create clientset: %w", err)
	}

	return &Client{
		clientset: clientset,
		namespace: namespace,
	}, nil
}

// CreatePVC creates a PersistentVolumeClaim for build artifacts.
func (c *Client) CreatePVC(ctx context.Context, name, storageClass string, size string) error {
	_, err := c.clientset.CoreV1().PersistentVolumeClaims(c.namespace).Create(ctx, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: parseResourceQuantity(size),
				},
			},
		},
	}, metav1.CreateOptions{})

	if err != nil {
		return fmt.Errorf("create PVC %s: %w", name, err)
	}
	return nil
}

// CreateJob creates a Kubernetes Job for building a package.
func (c *Client) CreateJob(ctx context.Context, job *batchv1.Job) error {
	_, err := c.clientset.BatchV1().Jobs(c.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create job %s: %w", job.Name, err)
	}
	return nil
}

// DeleteJob deletes a Kubernetes Job and its associated pods.
func (c *Client) DeleteJob(ctx context.Context, name string) error {
	propagation := metav1.DeletePropagationForeground
	err := c.clientset.BatchV1().Jobs(c.namespace).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	if err != nil {
		return fmt.Errorf("delete job %s: %w", name, err)
	}
	return nil
}

// WaitForJob waits for a job to complete and returns logs.
func (c *Client) WaitForJob(ctx context.Context, name string, logWriter io.Writer, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	retries := 0
	maxRetries := 10
	for {
		watcher, err := c.clientset.BatchV1().Jobs(c.namespace).Watch(ctx, metav1.SingleObject(metav1.ObjectMeta{Name: name}))
		if err != nil {
			return fmt.Errorf("watch job %s: %w", name, err)
		}

		done, err := c.watchUntilDone(ctx, name, logWriter, watcher)
		watcher.Stop()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		// channel closed by server — re-watch with backoff
		retries++
		if retries >= maxRetries {
			return fmt.Errorf("watch channel closed %d times for job %s", retries, name)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for job %s", name)
		case <-time.After(time.Duration(retries) * time.Second):
			// exponential backoff: 1s, 2s, 3s, ...
		}
	}
}

// watchUntilDone processes watch events until job completes, fails, or channel closes.
// Returns (true, nil) on success, (false, err) on job failure, (false, nil) on channel close.
func (c *Client) watchUntilDone(ctx context.Context, name string, logWriter io.Writer, watcher watch.Interface) (bool, error) {
	for {
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("timeout waiting for job %s", name)
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return false, nil // channel closed — caller will retry
			}
			job, ok := event.Object.(*batchv1.Job)
			if !ok {
				continue
			}
			c.streamJobLogs(ctx, name, logWriter)
			if job.Status.Succeeded > 0 {
				return true, nil
			}
			if job.Status.Failed > 0 {
				return false, fmt.Errorf("job %s failed", name)
			}
		}
	}
}

// streamJobLogs streams logs from a job's pods to the writer.
func (c *Client) streamJobLogs(ctx context.Context, jobName string, logWriter io.Writer) {
	pods, err := c.clientset.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return
	}

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			req := c.clientset.CoreV1().Pods(c.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: container.Name,
				Follow:    false,
			})

			stream, err := req.Stream(ctx)
			if err != nil {
				continue
			}

			io.Copy(logWriter, stream)
			stream.Close()
		}
	}
}

// GetJobLogs retrieves logs from a completed or running job.
func (c *Client) GetJobLogs(ctx context.Context, jobName string) (string, error) {
	pods, err := c.clientset.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return "", fmt.Errorf("list pods for job %s: %w", jobName, err)
	}

	var logs string
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			req := c.clientset.CoreV1().Pods(c.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: container.Name,
			})

			stream, err := req.Stream(ctx)
			if err != nil {
				continue
			}

			data, _ := io.ReadAll(stream)
			logs += fmt.Sprintf("--- Pod: %s, Container: %s ---\n%s\n", pod.Name, container.Name, string(data))
			stream.Close()
		}
	}

	return logs, nil
}

// ListJobs lists all jobs in the namespace.
func (c *Client) ListJobs(ctx context.Context) (*batchv1.JobList, error) {
	jobs, err := c.clientset.BatchV1().Jobs(c.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	return jobs, nil
}

// WatchJobs returns a watch for job events.
func (c *Client) WatchJobs(ctx context.Context) (watch.Interface, error) {
	watcher, err := c.clientset.BatchV1().Jobs(c.namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("watch jobs: %w", err)
	}
	return watcher, nil
}

func parseResourceQuantity(s string) resource.Quantity {
	// MustParse panics on invalid input, which is appropriate since
	// invalid resource quantities should be caught at config validation time
	return resource.MustParse(s)
}
