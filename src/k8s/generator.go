package k8s

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	config "github.com/NurOS-Linux/apger/src/core"
)

// JobConfig holds конфигурацию для генерации Job манифеста.
type JobConfig struct {
	JobName         string
	PackageName     string
	PackageVersion  string
	Image           string
	ImagePullPolicy string // IfNotPresent, Always, Never
	Command         []string
	Args            []string
	Env             map[string]string
	// BuildFlags from apger.conf [build.packages] or [build.self]
	BuildFlags *config.BuildProfile
	// OOMKillLimits from apger.conf — nil or empty strings = use defaults (4 CPU / 4Gi)
	OOMKillLimits *config.OOMKillLimits
	// Dependencies — package list for dnf install in init container
	Dependencies []string
	PVCName      string
	PVCMountPath string
}

// DefaultDependencies returns the standard set of build dependencies for Fedora.
func DefaultDependencies() []string {
	return []string{
		"gcc", "gcc-c++", "make",
		"meson", "ninja-build",
		"cmake", "pkg-config",
		"git", "tar", "curl", "wget",
		"go", "cargo",
		"python3-devel", "python3-pip",
		"zstd", "zstd-devel",
		"mold", "binutils",
	}
}

// oomResources returns ResourceRequirements using OOMKillLimits from config.
// Empty CPU/memory strings fall back to defaults (4 CPU / 4Gi).
func oomResources(limits *config.OOMKillLimits) corev1.ResourceRequirements {
	cpuLimit := "4"
	memLimit := "4Gi"
	if limits != nil {
		if limits.CPU != "" {
			cpuLimit = limits.CPU
		}
		if limits.Memory != "" {
			memLimit = limits.Memory
		}
	}
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpuLimit),
			corev1.ResourceMemory: resource.MustParse(memLimit),
		},
	}
}

// pullPolicyFromString converts string to corev1.PullPolicy.
func pullPolicyFromString(s string) corev1.PullPolicy {
	switch s {
	case "Always":
		return corev1.PullAlways
	case "Never":
		return corev1.PullNever
	default:
		return corev1.PullIfNotPresent
	}
}

// GenerateBuildJob creates a Kubernetes Job manifest for building a package.
func GenerateBuildJob(cfg JobConfig) *batchv1.Job {
	falseVal := false

	envVars := []corev1.EnvVar{
		{Name: "PACKAGE_NAME", Value: cfg.PackageName},
		{Name: "PACKAGE_VERSION", Value: cfg.PackageVersion},
		{Name: "DESTDIR", Value: "/build/root"},
	}
	if cfg.BuildFlags != nil {
		envVars = append(envVars,
			corev1.EnvVar{Name: "CC", Value: cfg.BuildFlags.CC},
			corev1.EnvVar{Name: "CXX", Value: cfg.BuildFlags.CXX},
			corev1.EnvVar{Name: "CFLAGS", Value: cfg.BuildFlags.CFlags()},
			corev1.EnvVar{Name: "CXXFLAGS", Value: cfg.BuildFlags.CXXFlags()},
			corev1.EnvVar{Name: "LDFLAGS", Value: cfg.BuildFlags.LDFlags()},
		)
	}
	for k, v := range cfg.Env {
		envVars = append(envVars, corev1.EnvVar{Name: k, Value: v})
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "build-output", MountPath: cfg.PVCMountPath},
		{Name: "build-tmp", MountPath: "/build"},
	}
	volumes := []corev1.Volume{
		{
			Name: "build-output",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: cfg.PVCName},
			},
		},
		{Name: "build-tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	command := cfg.Command
	if len(command) == 0 {
		command = []string{"/bin/sh"}
	}
	args := cfg.Args
	if len(args) == 0 {
		args = []string{"-c", "echo build"}
	}

	depInstallCmd := "dnf install -y --setopt=install_weak_deps=False " + joinSpace(cfg.Dependencies) + " && dnf clean all"
	pullPolicy := pullPolicyFromString(cfg.ImagePullPolicy)
	res := oomResources(cfg.OOMKillLimits)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   cfg.JobName,
			Labels: map[string]string{"app": "apger", "package": cfg.PackageName, "version": cfg.PackageVersion, "stage": "build"},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: ptrInt32(3600),
			BackoffLimit:            ptrInt32(2),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "apger", "package": cfg.PackageName, "version": cfg.PackageVersion},
				},
				Spec: corev1.PodSpec{
					RestartPolicy:         corev1.RestartPolicyOnFailure,
					ActiveDeadlineSeconds: ptrInt64(7200),
					InitContainers: []corev1.Container{{
						Name:            "install-deps",
						Image:           cfg.Image,
						ImagePullPolicy: pullPolicy,
						Command:         []string{"/bin/sh"},
						Args:            []string{"-c", depInstallCmd},
						Resources:       res,
						VolumeMounts:    volumeMounts,
					}},
					Containers: []corev1.Container{{
						Name:            "builder",
						Image:           cfg.Image,
						ImagePullPolicy: pullPolicy,
						Command:         command,
						Args:            args,
						Env:             envVars,
						Resources:       res,
						VolumeMounts:    volumeMounts,
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: &falseVal,
							ReadOnlyRootFilesystem:   &falseVal,
						},
					}},
					Volumes: volumes,
				},
			},
		},
	}
}

// GenerateMultiStageJob creates a Job for the complete multistage build pipeline.
// selfFlags must come from apger.conf [build.self].
func GenerateMultiStageJob(cfg JobConfig, selfFlags config.BuildProfile) *batchv1.Job {
	falseVal := false
	pullPolicy := pullPolicyFromString(cfg.ImagePullPolicy)
	res := oomResources(cfg.OOMKillLimits)

	depInstallCmd := "dnf install -y --setopt=install_weak_deps=False " + joinSpace(cfg.Dependencies) + " && dnf clean all"

	script := fmt.Sprintf(`#!/bin/sh
set -e
echo "=== Installing dependencies ==="
%s
export CC=%s CXX=%s CFLAGS="%s" CXXFLAGS="%s" LDFLAGS="%s"
echo "=== Stage 1: Building apgbuild ==="
cd /src/apgbuild && meson setup build --prefix=/usr --buildtype=release && ninja -C build && ninja -C build install
echo "=== Stage 2: Building apger ==="
cd /src && meson setup build --prefix=/usr --buildtype=release && ninja -C build && ninja -C build install
echo "=== Stage 3: Starting TUI ==="
exec apger --tui
`, depInstallCmd, selfFlags.CC, selfFlags.CXX,
		selfFlags.CFlags(), selfFlags.CXXFlags(), selfFlags.LDFlags())

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   cfg.JobName,
			Labels: map[string]string{"app": "apger", "stage": "multistage"},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptrInt32(1),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "apger", "stage": "multistage"}},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{{
						Name:            "multistage-builder",
						Image:           cfg.Image,
						ImagePullPolicy: pullPolicy,
						Command:         []string{"/bin/sh"},
						Args:            []string{"-c", script},
						Resources:       res,
						VolumeMounts: []corev1.VolumeMount{
							{Name: "build-output", MountPath: cfg.PVCMountPath},
							{Name: "source-code", MountPath: "/src"},
						},
						SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &falseVal},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "build-output",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: cfg.PVCName},
							},
						},
						{Name: "source-code", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			},
		},
	}
}

// GeneratePVCManifest creates a PVC manifest for build artifacts.
func GeneratePVCManifest(name, storageClass, size string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"app": "apger"}},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(size)},
			},
		},
	}
}

func joinSpace(items []string) string {
	result := ""
	for _, item := range items {
		result += item + " "
	}
	return result
}

func ptrInt32(v int32) *int32 { return &v }
func ptrInt64(v int64) *int64 { return &v }
