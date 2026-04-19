package k8s

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	config "github.com/NurOS-Linux/apger/src/core"
)

// JobConfig holds configuration for generating a build Job manifest.
type JobConfig struct {
	JobName        string
	PackageName    string
	PackageVersion string
	Image          string
	ImagePullPolicy string
	// Command/Args override the default apgbuild invocation (optional)
	Command []string
	Args    []string
	Env     map[string]string
	// BuildFlags from apger.conf [build.packages]
	BuildFlags *config.BuildProfile
	// OOMKillLimits from apger.conf
	OOMKillLimits *config.OOMKillLimits
	// Dependencies for dnf install in init container
	Dependencies []string
	PVCName      string
	// PVCMountPath is where built .apg packages are written (e.g. /output/packages)
	PVCMountPath string
}

// DefaultDependencies returns the standard Fedora build dependencies.
func DefaultDependencies() []string {
	return []string{
		"gcc", "gcc-c++", "make",
		"meson", "ninja-build", "cmake", "pkg-config",
		"git", "tar", "curl", "wget",
		"go", "cargo",
		"python3-devel", "python3-pip",
		"zstd", "zstd-devel",
		"mold", "binutils",
	}
}

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

// GenerateBuildJob creates a Job that:
//  1. Installs build deps (init container)
//  2. Runs apgbuild from /tools/bin/ (copied to PVC by apger at startup)
//     Output .apg goes to PVCMountPath/packages/
//
// The PVC is mounted read-write; /tools/ subpath provides apgbuild + libapg.so.
func GenerateBuildJob(cfg JobConfig) *batchv1.Job {
	falseVal := false

	envVars := []corev1.EnvVar{
		{Name: "PACKAGE_NAME", Value: cfg.PackageName},
		{Name: "PACKAGE_VERSION", Value: cfg.PackageVersion},
		{Name: "DESTDIR", Value: "/build/root"},
		// apgbuild and libapg.so live in /tools (subpath of PVC)
		{Name: "PATH", Value: "/tools/bin:/usr/local/bin:/usr/bin:/bin"},
		{Name: "LD_LIBRARY_PATH", Value: "/tools/lib"},
	}
	if cfg.BuildFlags != nil {
		envVars = append(envVars,
			corev1.EnvVar{Name: "CC", Value: cfg.BuildFlags.ResolvedCC()},
			corev1.EnvVar{Name: "CXX", Value: cfg.BuildFlags.ResolvedCXX()},
			corev1.EnvVar{Name: "CFLAGS", Value: cfg.BuildFlags.CFlags()},
			corev1.EnvVar{Name: "CXXFLAGS", Value: cfg.BuildFlags.CXXFlags()},
			corev1.EnvVar{Name: "LDFLAGS", Value: cfg.BuildFlags.LDFlags()},
		)
	}
	for k, v := range cfg.Env {
		envVars = append(envVars, corev1.EnvVar{Name: k, Value: v})
	}

	volumeMounts := []corev1.VolumeMount{
		// Full PVC: /output/tools/bin/apgbuild, /output/tools/lib/libapg.so, /output/packages/
		{Name: "pvc", MountPath: "/output"},
		// Symlink /tools → /output/tools for convenience (done in args)
		{Name: "build-tmp", MountPath: "/build"},
	}
	volumes := []corev1.Volume{
		{
			Name: "pvc",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: cfg.PVCName},
			},
		},
		{Name: "build-tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	// Default command: run apgbuild from PVC tools
	command := cfg.Command
	args := cfg.Args
	if len(command) == 0 {
		command = []string{"/bin/sh", "-c"}
		outFile := fmt.Sprintf("/output/packages/%s-%s-$(uname -m).apg", cfg.PackageName, cfg.PackageVersion)
		args = []string{fmt.Sprintf(
			`set -e
ln -sf /output/tools /tools
mkdir -p /output/packages
apgbuild build /build/src -o %s`, outFile)}
	}

	depInstallCmd := "dnf install -y --setopt=install_weak_deps=False " + joinSpace(cfg.Dependencies) + " && dnf clean all"
	pullPolicy := pullPolicyFromString(cfg.ImagePullPolicy)
	res := oomResources(cfg.OOMKillLimits)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: cfg.JobName,
			Labels: map[string]string{
				"app":     "apger",
				"package": cfg.PackageName,
				"version": cfg.PackageVersion,
				"stage":   "build",
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: ptrInt32(3600),
			BackoffLimit:            ptrInt32(3),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     "apger",
						"package": cfg.PackageName,
						"version": cfg.PackageVersion,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyOnFailure,
					ActiveDeadlineSeconds:        ptrInt64(7200),
					AutomountServiceAccountToken: &falseVal,
					InitContainers: []corev1.Container{{
						Name:            "install-deps",
						Image:           cfg.Image,
						ImagePullPolicy: pullPolicy,
						Command:         []string{"/bin/sh", "-c"},
						Args:            []string{depInstallCmd},
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
						},
					}},
					Volumes: volumes,
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
