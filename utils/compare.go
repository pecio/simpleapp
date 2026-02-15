package utils

import (
	"log"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func DeploymentEqual(d1, d2 appsv1.Deployment) bool {
	// The easy part - Replicas
	if *d1.Spec.Replicas != *d2.Spec.Replicas {
		return false
	}

	// Pod Templates
	t1s := d1.Spec.Template.Spec.DeepCopy()
	t2s := d2.Spec.Template.Spec.DeepCopy()
	// Image
	if t1s.Containers[0].Image != t2s.Containers[0].Image {
		return false
	}
	// Env
	if len(t1s.Containers[0].Env) != len(t2s.Containers[0].Env) {
		return false
	}
	for i, e := range t1s.Containers[0].Env {
		if t2s.Containers[0].Env[i] != e {
			return false
		}
	}
	// Ports
	if len(t1s.Containers[0].Ports) != len(t2s.Containers[0].Ports) {
		return false
	}
	for i, p := range t1s.Containers[0].Ports {
		if t2s.Containers[0].Ports[i] != p {
			return false
		}
	}
	// VolumeMounts
	if len(t1s.Containers[0].VolumeMounts) != len(t2s.Containers[0].VolumeMounts) {
		return false
	}
	for i, vM := range t1s.Containers[0].VolumeMounts {
		if t2s.Containers[0].VolumeMounts[i].MountPath != vM.MountPath {
			return false
		}
	}
	// Volumes
	if len(t1s.Volumes) != len(t2s.Volumes) { // Double check
		return false
	}
	for i, v := range t1s.Volumes {
		if !volumesEqual(t2s.Volumes[i], v) {
			log.Println("Volumes changed")
			return false
		}
	}

	return true
}

func volumesEqual(v1, v2 corev1.Volume) bool {
	if v1.Name != v2.Name {
		return false
	}
	// We only check the sources we use

	// ConfigMap
	if v1.ConfigMap != nil {
		if v2.ConfigMap == nil {
			return false
		}
		if v1.ConfigMap.Name != v2.ConfigMap.Name {
			return false
		}
		if len(v1.ConfigMap.Items) != len(v2.ConfigMap.Items) {
			return false
		}
		for i, item := range v1.ConfigMap.Items {
			if v2.ConfigMap.Items[i] != item {
				return false
			}
		}
		if v1.ConfigMap.DefaultMode != nil || v2.ConfigMap.DefaultMode != nil {
			// Default DefaultMode is 420 (octal 644)
			if v1.ConfigMap.DefaultMode == nil && *v2.ConfigMap.DefaultMode != 420 {
				return false
			}
			if v2.ConfigMap.DefaultMode == nil && *v1.ConfigMap.DefaultMode != 420 {
				return false
			}
			if *v1.ConfigMap.DefaultMode != *v2.ConfigMap.DefaultMode {
				return false
			}
		}
		if defaultBool(v1.ConfigMap.Optional, false) != defaultBool(v2.ConfigMap.Optional, false) {
			return false
		}
	}

	// CSI
	if v1.CSI != nil {
		if v2.CSI == nil {
			return false
		}
		if v1.CSI.Driver != v2.CSI.Driver {
			return false
		}
		if defaultString(v1.CSI.FSType, "") != defaultString(v2.CSI.FSType, "") {
			return false
		}
		if v1.CSI.NodePublishSecretRef != nil || v2.CSI.NodePublishSecretRef != nil {
			if v1.CSI.NodePublishSecretRef == nil || v2.CSI.NodePublishSecretRef == nil {
				return false
			}
			if v1.CSI.NodePublishSecretRef.Name != v2.CSI.NodePublishSecretRef.Name {
				return false
			}
		}
		if defaultBool(v1.CSI.ReadOnly, false) != defaultBool(v2.CSI.ReadOnly, false) {
			return false
		}
		if len(v1.CSI.VolumeAttributes) != len(v2.CSI.VolumeAttributes) {
			return false
		}
		for key, value := range v1.CSI.VolumeAttributes {
			if v2.CSI.VolumeAttributes[key] != value {
				return false
			}
		}
	}

	// emptyDir
	if v1.EmptyDir != nil {
		if v2.EmptyDir == nil {
			return false
		}
		if v1.EmptyDir.Medium != v2.EmptyDir.Medium {
			return false
		}
		if v1.EmptyDir.SizeLimit != nil {
			if v2.EmptyDir.SizeLimit == nil {
				return false
			}
			if *v1.EmptyDir.SizeLimit != *v2.EmptyDir.SizeLimit {
				return false
			}
		}
	}

	// PersistentVolumeClaim
	if v1.PersistentVolumeClaim != nil {
		if v2.PersistentVolumeClaim == nil {
			return false
		}
		if v1.PersistentVolumeClaim.ClaimName != v2.PersistentVolumeClaim.ClaimName {
			return false
		}
		if v1.PersistentVolumeClaim.ReadOnly != v2.PersistentVolumeClaim.ReadOnly {
			return false
		}
	}

	// Secret
	if v1.Secret != nil {
		if v2.Secret == nil {
			return false
		}
		if v1.Secret.SecretName != v2.Secret.SecretName {
			return false
		}
		if len(v1.Secret.Items) != len(v2.Secret.Items) {
			return false
		}
		for i, item := range v1.Secret.Items {
			if v2.Secret.Items[i] != item {
				return false
			}
		}
		if defaultBool(v1.Secret.Optional, false) != defaultBool(v2.Secret.Optional, false) {
			return false
		}
	}

	return true
}

func defaultBool(b *bool, def bool) bool {
	if b == nil {
		return def
	}
	return *b
}

func defaultString(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}
