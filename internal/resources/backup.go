/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

// VolumeSnapshotGVK is the CSI external-snapshotter VolumeSnapshot GVK. Built
// unstructured to avoid a hard dependency on the snapshot API module.
var VolumeSnapshotGVK = schema.GroupVersionKind{
	Group:   "snapshot.storage.k8s.io",
	Version: "v1",
	Kind:    "VolumeSnapshot",
}

// SnapshotName returns the VolumeSnapshot object name for a backup.
func SnapshotName(b *palworldv1alpha1.PalworldBackup) string { return b.Name }

// RestorePVCName is the temporary PVC cloned from a snapshot for tar/upload jobs.
func RestorePVCName(name string) string { return name + "-src" }

// BackupJobName / RestoreJobName name the worker Jobs.
func BackupJobName(name string) string  { return name + "-upload" }
func RestoreJobName(name string) string { return name + "-restore" }

// DesiredVolumeSnapshot builds a VolumeSnapshot of a game's data PVC.
func DesiredVolumeSnapshot(g *palworldv1alpha1.PalworldGame, b *palworldv1alpha1.PalworldBackup) *unstructured.Unstructured {
	spec := map[string]any{
		"source": map[string]any{
			"persistentVolumeClaimName": DataPVCName(g),
		},
	}
	if g.Spec.Storage.VolumeSnapshotClassName != nil && *g.Spec.Storage.VolumeSnapshotClassName != "" {
		spec["volumeSnapshotClassName"] = *g.Spec.Storage.VolumeSnapshotClassName
	}
	labels := map[string]any{}
	for k, v := range CommonLabels(g) {
		labels[k] = v
	}
	snap := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name":      SnapshotName(b),
				"namespace": b.Namespace,
				"labels":    labels,
			},
			"spec": spec,
		},
	}
	snap.SetGroupVersionKind(VolumeSnapshotGVK)
	return snap
}

// SnapshotReady reports whether a VolumeSnapshot is ready to use and its restore
// size in bytes (0 if unknown).
func SnapshotReady(u *unstructured.Unstructured) (bool, int64) {
	ready, _, _ := unstructured.NestedBool(u.Object, "status", "readyToUse")
	sizeStr, found, _ := unstructured.NestedString(u.Object, "status", "restoreSize")
	var size int64
	if found {
		if q, err := resource.ParseQuantity(sizeStr); err == nil {
			size = q.Value()
		}
	}
	return ready, size
}

// DesiredRestorePVCFromSnapshot builds a PVC provisioned from a snapshot, used
// as a contention-free source for tar/upload jobs.
func DesiredRestorePVCFromSnapshot(g *palworldv1alpha1.PalworldGame, name, snapshotName string) *corev1.PersistentVolumeClaim {
	size := g.Spec.Storage.Size
	if size.IsZero() {
		size = resource.MustParse("20Gi")
	}
	apiGroup := "snapshot.storage.k8s.io"
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: g.Namespace,
			Labels:    CommonLabels(g),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: size},
			},
			StorageClassName: g.Spec.Storage.StorageClassName,
			DataSource: &corev1.TypedLocalObjectReference{
				APIGroup: &apiGroup,
				Kind:     "VolumeSnapshot",
				Name:     snapshotName,
			},
		},
	}
}

func s3Env(dest *palworldv1alpha1.S3Destination, key string) []corev1.EnvVar {
	if dest == nil {
		return nil
	}
	accessKeyID := dest.AccessKeyIDKey
	if accessKeyID == "" {
		accessKeyID = "AWS_ACCESS_KEY_ID"
	}
	secretKey := dest.SecretAccessKeyKey
	if secretKey == "" {
		secretKey = "AWS_SECRET_ACCESS_KEY"
	}
	env := []corev1.EnvVar{
		{Name: "S3_BUCKET", Value: dest.Bucket},
		{Name: "S3_PREFIX", Value: dest.Prefix},
		{Name: "S3_ENDPOINT", Value: dest.Endpoint},
		{Name: "S3_REGION", Value: dest.Region},
		{Name: "S3_KEY", Value: key},
	}
	if dest.InsecureTLS {
		env = append(env, corev1.EnvVar{Name: "S3_INSECURE_TLS", Value: "true"})
	}
	env = append(env,
		corev1.EnvVar{Name: "AWS_ACCESS_KEY_ID", ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: dest.CredentialsSecret},
				Key:                  accessKeyID,
			},
		}},
		corev1.EnvVar{Name: "AWS_SECRET_ACCESS_KEY", ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: dest.CredentialsSecret},
				Key:                  secretKey,
			},
		}},
	)
	return env
}

// JobSpec describes a backup/restore worker job.
type JobSpec struct {
	Name        string
	Image       string
	Command     []string
	Env         []corev1.EnvVar
	SourcePVC   string // mounted read/write at /palworld
	BackupPVC   string // optional, mounted at /backup
	OpenShift   bool
	ServiceName string
}

func opsPodSecurityContext(openShift bool) *corev1.PodSecurityContext {
	runAsNonRoot := true
	sc := &corev1.PodSecurityContext{
		RunAsNonRoot:   &runAsNonRoot,
		SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
	if !openShift {
		uid := int64(10000)
		gid := int64(0)
		fsGroup := int64(0)
		sc.RunAsUser = &uid
		sc.RunAsGroup = &gid
		sc.FSGroup = &fsGroup
	}
	return sc
}

// buildWorkerJob assembles a backup or restore Job.
func buildWorkerJob(g *palworldv1alpha1.PalworldGame, js JobSpec) *batchv1.Job {
	backoff := int32(3)
	ttl := int32(3600)
	mounts := []corev1.VolumeMount{{Name: "data", MountPath: DataMountPath}}
	volumes := []corev1.Volume{
		{Name: "data", VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: js.SourcePVC},
		}},
	}
	if js.BackupPVC != "" {
		mounts = append(mounts, corev1.VolumeMount{Name: "backup", MountPath: "/backup"})
		volumes = append(volumes, corev1.Volume{Name: "backup", VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: js.BackupPVC},
		}})
	}
	sa := ServiceAccountName(g)
	if g.Spec.ServiceAccountName != "" {
		sa = g.Spec.ServiceAccountName
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      js.Name,
			Namespace: g.Namespace,
			Labels:    CommonLabels(g),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: CommonLabels(g)},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: sa,
					SecurityContext:    opsPodSecurityContext(js.OpenShift),
					ImagePullSecrets:   g.Spec.Image.PullSecrets,
					Containers: []corev1.Container{{
						Name:            "worker",
						Image:           js.Image,
						ImagePullPolicy: pullPolicy(g),
						Command:         js.Command,
						Env:             append([]corev1.EnvVar{{Name: "STEAMAPPDIR", Value: DataMountPath}}, js.Env...),
						VolumeMounts:    mounts,
						SecurityContext: containerSecurityContext(),
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					}},
					Volumes: volumes,
				},
			},
		},
	}
}

// DesiredBackupJob builds a Job that tars the (snapshot-cloned) save volume to
// S3 or a backup PVC.
func DesiredBackupJob(g *palworldv1alpha1.PalworldGame, b *palworldv1alpha1.PalworldBackup, image, srcPVC string, openShift bool) *batchv1.Job {
	js := JobSpec{
		Name:      BackupJobName(b.Name),
		Image:     image,
		SourcePVC: srcPVC,
		OpenShift: openShift,
	}
	switch b.Spec.Destination.Type {
	case palworldv1alpha1.BackupDestinationS3:
		key := b.Name + ".tar.gz"
		if b.Spec.Destination.S3 != nil {
			js.Command = []string{"/usr/local/bin/backup.sh", "s3"}
			js.Env = s3Env(b.Spec.Destination.S3, g.Name+"/"+key)
		}
	case palworldv1alpha1.BackupDestinationPVC:
		js.BackupPVC = b.Spec.Destination.PVCName
		js.Command = []string{"/usr/local/bin/backup.sh", "pvc", "/backup/" + g.Name + "/" + b.Name + ".tar.gz"}
	}
	return buildWorkerJob(g, js)
}

// DesiredRestoreJob builds a Job that restores a tarball onto the live data PVC.
func DesiredRestoreJob(g *palworldv1alpha1.PalworldGame, name, image, key string, dest palworldv1alpha1.BackupDestination, openShift bool) *batchv1.Job {
	js := JobSpec{
		Name:      RestoreJobName(name),
		Image:     image,
		SourcePVC: DataPVCName(g),
		OpenShift: openShift,
	}
	switch dest.Type {
	case palworldv1alpha1.BackupDestinationS3:
		js.Command = []string{"/usr/local/bin/restore.sh", "s3"}
		js.Env = s3Env(dest.S3, key)
	case palworldv1alpha1.BackupDestinationPVC:
		js.BackupPVC = dest.PVCName
		js.Command = []string{"/usr/local/bin/restore.sh", "pvc", "/backup/" + key}
	}
	return buildWorkerJob(g, js)
}
