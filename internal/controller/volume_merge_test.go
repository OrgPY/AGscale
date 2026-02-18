package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestMergeVolumesOverrideByName(t *testing.T) {
	base := []corev1.Volume{{Name: "shared", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}, {Name: "base-only", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
	override := []corev1.Volume{{Name: "shared", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{}}}, {Name: "agent-only", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
	merged := mergeVolumes(base, override)
	if len(merged) != 3 {
		t.Fatalf("expected 3 volumes, got %d", len(merged))
	}
	if merged[0].Name != "shared" || merged[0].ConfigMap == nil {
		t.Fatalf("expected shared volume to be overridden by agent volume")
	}
}

func TestMergeVolumeMountsOverrideByName(t *testing.T) {
	base := []corev1.VolumeMount{{Name: "shared", MountPath: "/base"}, {Name: "base-only", MountPath: "/base-only"}}
	override := []corev1.VolumeMount{{Name: "shared", MountPath: "/agent"}, {Name: "agent-only", MountPath: "/agent-only"}}
	merged := mergeVolumeMounts(base, override)
	if len(merged) != 3 {
		t.Fatalf("expected 3 mounts, got %d", len(merged))
	}
	if merged[0].Name != "shared" || merged[0].MountPath != "/agent" {
		t.Fatalf("expected shared mount to be overridden by agent mount")
	}
}
