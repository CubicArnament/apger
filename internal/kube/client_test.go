package kube

import (
	"context"
	"slices"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/NurOS-Linux/apger/internal/config"
)

func TestBuildJobIsIsolated(t *testing.T) {
	client := &Client{config: config.Config{
		Namespace: "apger", BuilderImage: "example/apger:test", ImagePullPolicy: "IfNotPresent",
		OutputPVC: "output", JobTTL: time.Hour, BuildTimeout: 30 * time.Minute,
	}}
	job := client.buildJob("apger-hello-1234", "hello")
	pod := job.Spec.Template.Spec
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatal("worker must not mount a service account token")
	}
	container := pod.Containers[0]
	if slices.Contains(container.Args, "/cache") {
		t.Fatalf("cache argument present without cache PVC: %#v", container.Args)
	}
	if container.SecurityContext == nil || container.SecurityContext.ReadOnlyRootFilesystem == nil || !*container.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("worker root filesystem must be read-only")
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Fatal("package builds must not retry automatically")
	}
	if container.Resources.Limits.StorageEphemeral().IsZero() {
		t.Fatal("worker must have an ephemeral-storage limit")
	}
	for _, volume := range pod.Volumes {
		if (volume.Name == "work" || volume.Name == "tmp") && (volume.EmptyDir == nil || volume.EmptyDir.SizeLimit == nil) {
			t.Fatalf("%s emptyDir must have a size limit", volume.Name)
		}
	}
}

func TestBuildJobMountsConfiguredCache(t *testing.T) {
	client := &Client{config: config.Config{
		Namespace: "apger", BuilderImage: "example/apger:test", ImagePullPolicy: "IfNotPresent",
		OutputPVC: "output", CachePVC: "cache", JobTTL: time.Hour, BuildTimeout: 30 * time.Minute,
	}}
	job := client.buildJob("apger-hello-1234", "hello")
	if !slices.Contains(job.Spec.Template.Spec.Containers[0].Args, "/cache") {
		t.Fatal("cache argument missing")
	}
}

func TestClientDoesNotExposeUnmanagedJobs(t *testing.T) {
	ctx := context.Background()
	backend := fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "unmanaged", Namespace: "apger"},
	})
	client := NewWithClient(backend, config.Config{Namespace: "apger"})

	if _, err := client.Get(ctx, "unmanaged"); !apierrors.IsNotFound(err) {
		t.Fatalf("Get error = %v, want NotFound", err)
	}
	if err := client.Delete(ctx, "unmanaged"); !apierrors.IsNotFound(err) {
		t.Fatalf("Delete error = %v, want NotFound", err)
	}
	if _, err := backend.BatchV1().Jobs("apger").Get(ctx, "unmanaged", metav1.GetOptions{}); err != nil {
		t.Fatalf("unmanaged Job was modified: %v", err)
	}
}
