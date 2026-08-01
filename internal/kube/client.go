package kube

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/NurOS-Linux/apger/internal/config"
)

const (
	managedLabel = "app.kubernetes.io/managed-by"
	buildLabel   = "apger.nuros.org/build"
	packageLabel = "apger.nuros.org/package"
)

type Client struct {
	client kubernetes.Interface
	config config.Config
}

func New(cfg config.Config) (*Client, error) {
	restConfig, err := kubernetesConfig(cfg.Kubeconfig)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return &Client{client: client, config: cfg}, nil
}

func NewWithClient(client kubernetes.Interface, cfg config.Config) *Client {
	return &Client{client: client, config: cfg}
}

func kubernetesConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig == "" {
		config, err := rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("load in-cluster Kubernetes config: %w", err)
		}
		return config, nil
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	return config, nil
}

func (c *Client) Submit(ctx context.Context, options SubmitOptions) (Build, error) {
	packageName := dnsLabel(options.Package)
	if packageName == "" {
		return Build{}, fmt.Errorf("package name must contain letters or numbers")
	}
	id, err := buildID(packageName)
	if err != nil {
		return Build{}, err
	}
	immutable := true
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: id, Namespace: c.config.Namespace, Labels: objectLabels(id, packageName)},
		Immutable:  &immutable,
		Data:       map[string]string{"PKGBUILD": options.PKGBUILD},
	}
	if _, err := c.client.CoreV1().ConfigMaps(c.config.Namespace).Create(ctx, configMap, metav1.CreateOptions{}); err != nil {
		return Build{}, fmt.Errorf("create PKGBUILD ConfigMap: %w", err)
	}
	job := c.buildJob(id, packageName)
	created, err := c.client.BatchV1().Jobs(c.config.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		_ = c.client.CoreV1().ConfigMaps(c.config.Namespace).Delete(ctx, id, metav1.DeleteOptions{})
		return Build{}, fmt.Errorf("create build Job: %w", err)
	}
	configMap.OwnerReferences = []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: created.Name, UID: created.UID, Controller: boolPointer(true), BlockOwnerDeletion: boolPointer(true)}}
	if _, err := c.client.CoreV1().ConfigMaps(c.config.Namespace).Update(ctx, configMap, metav1.UpdateOptions{}); err != nil {
		_ = c.client.BatchV1().Jobs(c.config.Namespace).Delete(ctx, id, metav1.DeleteOptions{PropagationPolicy: propagation(metav1.DeletePropagationForeground)})
		return Build{}, fmt.Errorf("attach PKGBUILD ConfigMap to Job: %w", err)
	}
	return buildFromJob(created), nil
}

func (c *Client) Get(ctx context.Context, id string) (Build, error) {
	job, err := c.managedJob(ctx, id)
	if err != nil {
		return Build{}, err
	}
	return buildFromJob(job), nil
}

func (c *Client) managedJob(ctx context.Context, id string) (*batchv1.Job, error) {
	job, err := c.client.BatchV1().Jobs(c.config.Namespace).Get(ctx, id, metav1.GetOptions{})
	if err != nil {
		return nil, mapNotFound(err, id)
	}
	if job.Labels[managedLabel] != "apger" {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "batch", Resource: "builds"}, id)
	}
	return job, nil
}

func (c *Client) List(ctx context.Context) ([]Build, error) {
	selector := labels.Set{managedLabel: "apger"}.AsSelector().String()
	jobs, err := c.client.BatchV1().Jobs(c.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list build Jobs: %w", err)
	}
	builds := make([]Build, 0, len(jobs.Items))
	for index := range jobs.Items {
		builds = append(builds, buildFromJob(&jobs.Items[index]))
	}
	sort.Slice(builds, func(i, j int) bool { return builds[i].CreatedAt.After(builds[j].CreatedAt) })
	return builds, nil
}

func (c *Client) Logs(ctx context.Context, id string) (string, error) {
	if _, err := c.managedJob(ctx, id); err != nil {
		return "", err
	}
	selector := labels.Set{buildLabel: id}.AsSelector().String()
	pods, err := c.client.CoreV1().Pods(c.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return "", fmt.Errorf("list build Pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("build %q has no Pod yet", id)
	}
	sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].CreationTimestamp.Before(&pods.Items[j].CreationTimestamp) })
	request := c.client.CoreV1().Pods(c.config.Namespace).GetLogs(pods.Items[len(pods.Items)-1].Name, &corev1.PodLogOptions{Container: "builder"})
	stream, err := request.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("open build logs: %w", err)
	}
	defer stream.Close()
	data, err := io.ReadAll(io.LimitReader(stream, 10<<20))
	if err != nil {
		return "", fmt.Errorf("read build logs: %w", err)
	}
	return string(data), nil
}

func (c *Client) Delete(ctx context.Context, id string) error {
	if _, err := c.managedJob(ctx, id); err != nil {
		return err
	}
	policy := metav1.DeletePropagationForeground
	if err := c.client.BatchV1().Jobs(c.config.Namespace).Delete(ctx, id, metav1.DeleteOptions{PropagationPolicy: &policy}); err != nil {
		return mapNotFound(err, id)
	}
	return nil
}

func (c *Client) buildJob(id, packageName string) *batchv1.Job {
	ttl := int32(c.config.JobTTL.Seconds())
	deadline := int64(c.config.BuildTimeout.Seconds())
	backoff := int32(0)
	nonRoot := true
	uid := int64(1000)
	readOnly := true
	allowEscalation := false
	workLimit := resource.MustParse("10Gi")
	tmpLimit := resource.MustParse("2Gi")
	labels := objectLabels(id, packageName)
	volumes := []corev1.Volume{
		{Name: "recipe", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: id}}}},
		{Name: "work", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &workLimit}}},
		{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &tmpLimit}}},
		{Name: "output", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: c.config.OutputPVC}}},
	}
	mounts := []corev1.VolumeMount{
		{Name: "recipe", MountPath: "/recipe", ReadOnly: true},
		{Name: "work", MountPath: "/work"},
		{Name: "tmp", MountPath: "/tmp"},
		{Name: "output", MountPath: "/output"},
	}
	workerArgs := []string{"worker", "--pkgbuild", "/recipe/PKGBUILD", "--work", "/work", "--output", "/output"}
	if c.config.CachePVC != "" {
		volumes = append(volumes, corev1.Volume{Name: "cache", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: c.config.CachePVC}}})
		mounts = append(mounts, corev1.VolumeMount{Name: "cache", MountPath: "/cache"})
		workerArgs = append(workerArgs, "--cache", "/cache")
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: id, Namespace: c.config.Namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoff, TTLSecondsAfterFinished: &ttl, ActiveDeadlineSeconds: &deadline,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: boolPointer(false), Volumes: volumes,
					SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &nonRoot, RunAsUser: &uid, RunAsGroup: &uid, FSGroup: &uid},
					Containers: []corev1.Container{{
						Name: "builder", Image: c.config.BuilderImage, ImagePullPolicy: corev1.PullPolicy(c.config.ImagePullPolicy),
						Args:            workerArgs,
						Env:             []corev1.EnvVar{{Name: "HOME", Value: "/work/home"}, {Name: "XDG_CACHE_HOME", Value: "/work/cache"}},
						VolumeMounts:    mounts,
						SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &allowEscalation, ReadOnlyRootFilesystem: &readOnly, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:              resource.MustParse("500m"),
								corev1.ResourceMemory:           resource.MustParse("512Mi"),
								corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:              resource.MustParse("4"),
								corev1.ResourceMemory:           resource.MustParse("4Gi"),
								corev1.ResourceEphemeralStorage: resource.MustParse("12Gi"),
							},
						},
					}},
				},
			},
		},
	}
}

func buildFromJob(job *batchv1.Job) Build {
	build := Build{ID: job.Name, Package: job.Labels[packageLabel], State: "pending", CreatedAt: job.CreationTimestamp.Time}
	if job.Status.StartTime != nil {
		value := job.Status.StartTime.Time
		build.StartedAt = &value
		build.State = "running"
	}
	if job.Status.CompletionTime != nil {
		value := job.Status.CompletionTime.Time
		build.EndedAt = &value
	}
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			build.State = "succeeded"
			build.Message = condition.Message
		case batchv1.JobFailed:
			build.State = "failed"
			build.Message = condition.Message
		}
	}
	return build
}

func objectLabels(id, packageName string) map[string]string {
	return map[string]string{managedLabel: "apger", "app.kubernetes.io/name": "apger-worker", buildLabel: id, packageLabel: packageName}
}

var invalidDNS = regexp.MustCompile(`[^a-z0-9-]+`)

func dnsLabel(value string) string {
	value = invalidDNS.ReplaceAllString(strings.ToLower(value), "-")
	value = strings.Trim(value, "-")
	if len(value) > 40 {
		value = strings.Trim(value[:40], "-")
	}
	return value
}

func buildID(packageName string) (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate build ID: %w", err)
	}
	return "apger-" + packageName + "-" + hex.EncodeToString(random), nil
}

func mapNotFound(err error, id string) error {
	if apierrors.IsNotFound(err) {
		return apierrors.NewNotFound(schema.GroupResource{Group: "batch", Resource: "builds"}, id)
	}
	return err
}

func boolPointer(value bool) *bool                                             { return &value }
func propagation(value metav1.DeletionPropagation) *metav1.DeletionPropagation { return &value }
