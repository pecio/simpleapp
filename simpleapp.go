package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"log"
	"regexp"
	"strings"

	"github.com/pecio/simpleapp/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
)

const (
	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "simpleapp"
	resourcePath   = "apps.raulpedroche.es/v1alpha1"
	singular       = "SimpleApp"
	plural         = "SimpleApps"
)

type SimpleAppList struct {
	ApiVersion string      `json:"apiVersion"`
	Items      []SimpleApp `json:"items"`
	Kind       string      `json:"kind"`
	Metadata   interface{} `json:"metadata"`
}

type SimpleApp struct {
	ApiVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`

	Metadata metav1.ObjectMeta `json:"metadata"`
	Spec     simpleAppSpec     `json:"spec,omitempty"`
}

type simpleAppSpec struct {
	Image    string            `json:"image"`
	Replicas *int32            `json:"replicas,omitempty"`
	Ports    []simpleAppPort   `json:"ports,omitempty"`
	Env      []corev1.EnvVar   `json:"env,omitempty"`
	Volumes  []simpleAppVolume `json:"volumes,omitempty"`
}

type simpleAppPort struct {
	Name          string          `json:"name,omitempty"`
	HostPort      int32           `json:"hostPort"`
	ContainerPort int32           `json:"containerPort"`
	Protocol      corev1.Protocol `json:"protocol,omitempty"`
}

type simpleAppVolume struct {
	MountPath             string                                `json:"mountPath"`
	EmptyDir              *simpleAppVolumeEmptyDir              `json:"emptyDir,omitempty"`
	ConfigMap             *simpleAppVolumeConfigMapOrSecret     `json:"configMap,omitempty"`
	PersistentVolumeClaim *simpleAppVolumePersistentVolumeClaim `json:"persistentVolumeClaim,omitempty"`
	Secret                *simpleAppVolumeConfigMapOrSecret     `json:"secret,omitempty"`
	CSI                   *corev1.CSIVolumeSource               `json:"csi,omitempty"`
}

type simpleAppVolumeEmptyDir struct {
	Medium    corev1.StorageMedium `json:"medium,omitempty"`
	SizeLimit *resource.Quantity   `json:"sizeLimit,omitempty"`
}

type simpleAppVolumeConfigMapOrSecret struct {
	DefaultMode *int32             `json:"defaultMode,omitempty"`
	Items       []corev1.KeyToPath `json:"items,omitempty"`
	Name        string             `json:"name"`
	Optional    *bool              `json:"optional,omitempty"`
}

type simpleAppVolumePersistentVolumeClaim struct {
	ClaimName string `json:"claimName"`
	ReadOnly  *bool  `json:"readOnly,omitempty"`
}

func (sa *SimpleApp) createOrUpdate(clientset *kubernetes.Clientset) error {
	// Sanity checks - fix duplicate Volumes or Ports
	fv := sa.fixVolumes()
	fp := sa.fixPorts()
	if fv || fp {
		sa.updateApp(clientset)
	}

	// Check if Deployment exists
	oldDeployment, err := clientset.AppsV1().Deployments(sa.Metadata.Namespace).Get(context.TODO(), sa.Metadata.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		deployment, err := sa.buildDeployment()
		if err != nil {
			return err
		}

		_, err = clientset.AppsV1().Deployments(sa.Metadata.Namespace).Create(context.TODO(), &deployment, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		log.Printf("Created Deployment %v.%v", sa.Metadata.Namespace, sa.Metadata.Name)
	} else if err != nil {
		return err
	} else {
		// Check if deployment is ours
		managedBy, ok := oldDeployment.ObjectMeta.Labels[managedByLabel]
		if !ok || managedBy != managedByValue {
			return fmt.Errorf("found Deployment %v.%v not managed by us", oldDeployment.ObjectMeta.Namespace, oldDeployment.ObjectMeta.Name)
		}

		newDeployment, err := sa.buildDeployment()
		if err != nil {
			return err
		}

		if !utils.DeploymentEqual(newDeployment, *oldDeployment) {
			_, err = clientset.AppsV1().Deployments(oldDeployment.ObjectMeta.Namespace).Update(context.TODO(), &newDeployment, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			log.Printf("Deployment %v.%v updated", oldDeployment.ObjectMeta.Namespace, oldDeployment.ObjectMeta.Name)
		}
	}

	// Check if Service exists
	oldService, err := clientset.CoreV1().Services(sa.Metadata.Namespace).Get(context.TODO(), sa.Metadata.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		// Create Service
		service, err := sa.buildService()
		if err != nil {
			return err
		}

		newService, err := clientset.CoreV1().Services(sa.Metadata.Namespace).Create(context.TODO(), &service, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		log.Printf("Created Service %v.%v", newService.ObjectMeta.Namespace, newService.ObjectMeta.Name)
	} else if err != nil {
		return err
	} else {
		managedBy, ok := oldService.ObjectMeta.Labels[managedByLabel]
		if !ok || managedBy != managedByValue {
			return fmt.Errorf("found Service %v.%v not managed by us", oldService.ObjectMeta.Namespace, oldService.ObjectMeta.Name)
		}

		newService, err := sa.buildService()
		if err != nil {
			return nil
		}

		if !utils.ServicesEqual(newService, *oldService) {
			_, err = clientset.CoreV1().Services(oldService.ObjectMeta.Namespace).Update(context.TODO(), &newService, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			log.Printf("Service %v.%v updated", oldService.ObjectMeta.Namespace, oldService.ObjectMeta.Name)
		}
	}
	return nil
}

func (sa *SimpleApp) buildService() (corev1.Service, error) {
	servicePorts := make([]corev1.ServicePort, 0, len(sa.Spec.Ports))

	for _, saPort := range sa.Spec.Ports {
		// Spec forces non-empty names if more than 1 port defined
		portName := saPort.Name
		if len(sa.Spec.Ports) > 1 && saPort.Name == "" {
			if saPort.Protocol == "" {
				portName = fmt.Sprintf("tcp-%s", saPort.ContainerPort)
			} else {
				portName = strings.ToLower(fmt.Sprintf("%s-%d", saPort.Protocol, saPort.ContainerPort))
			}
			// Check we have not generated a duplicate name
			for _, sp := range servicePorts {
				if portName == sp.Name {
					// Add b if it ends in a digit, increase end letter if it ends in a letter
					// That is "tcp-443" -> "tcp-443b", "tcp-443b" -> "tcp-443c"
					matched, err := regexp.MatchString("[0-9]$", portName)
					if err != nil {
						return corev1.Service{}, err
					}
					if matched {
						portName = portName + "b"
					} else {
						// This does support UTF-8 but we do not need it
						last := portName[len(portName)-1:][0] + 1
						// Safeguard: use hash if we have surpassed 'z'
						if last > 'z' {
							// The following should be unique as we have removed duplicate HostPorts
							suffix := rand.SafeEncodeString(fmt.Sprintf("%s-%d-%d", saPort.Protocol, saPort.HostPort, saPort.ContainerPort))
							portName = fmt.Sprintf("%s%s", portName[:len(portName)-1], suffix)
						} else {
							portName = fmt.Sprintf("%s%c", portName[:len(portName)-1], last)
						}
					}
				}
			}
		}

		servicePort := corev1.ServicePort{
			Name:       portName,
			Protocol:   saPort.Protocol,
			Port:       saPort.HostPort,
			TargetPort: intstr.FromInt32(saPort.ContainerPort),
		}
		servicePorts = append(servicePorts, servicePort)
	}
	service := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: sa.Metadata.Namespace,
			Name:      sa.Metadata.Name,
			Labels:    sa.labels(),
		},
		Spec: corev1.ServiceSpec{
			Selector: sa.labels(),
			Ports:    servicePorts,
			Type:     corev1.ServiceTypeNodePort,
		},
	}
	return service, nil
}

func (sa *SimpleApp) labels() map[string]string {
	labels := map[string]string{
		"app":          sa.Metadata.Name,
		managedByLabel: managedByValue,
	}
	return labels
}

func (sa *SimpleApp) buildDeployment() (appsv1.Deployment, error) {
	ports := make([]corev1.ContainerPort, 0, len(sa.Spec.Ports))
	for _, saPort := range sa.Spec.Ports {
		port := corev1.ContainerPort{
			ContainerPort: saPort.ContainerPort,
			Name:          saPort.Name,
			Protocol:      saPort.Protocol,
		}
		ports = append(ports, port)
	}
	volumes := make([]corev1.Volume, 0, len(sa.Spec.Volumes))
	volumeMounts := make([]corev1.VolumeMount, 0, len(sa.Spec.Volumes))

	for _, saVolume := range sa.Spec.Volumes {
		volume, volumeMount, err := sa.makeVolume(saVolume)
		if err != nil {
			return appsv1.Deployment{}, err
		}
		volumes = append(volumes, volume)
		volumeMounts = append(volumeMounts, volumeMount)
	}
	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			corev1.Container{
				Name:         sa.Metadata.Name,
				Image:        sa.Spec.Image,
				Ports:        ports,
				VolumeMounts: volumeMounts,
				Env:          sa.Spec.Env,
			},
		},
		Volumes: volumes,
	}
	selector := metav1.LabelSelector{}
	metav1.AddLabelToSelector(&selector, "app", sa.Metadata.Name)
	metav1.AddLabelToSelector(&selector, managedByLabel, managedByValue)
	deploymentSpec := appsv1.DeploymentSpec{
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: sa.labels(),
			},
			Spec: podSpec,
		},
		Selector: &selector,
		Replicas: sa.Spec.Replicas,
	}
	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:   sa.Metadata.Name,
			Labels: sa.labels(),
		},
		Spec: deploymentSpec,
	}
	return deployment, nil
}

func (sa *SimpleApp) makeVolume(saVolume simpleAppVolume) (corev1.Volume, corev1.VolumeMount, error) {
	// Use a simplified version of k8s.io/pkg/controller/ ComputeHash
	volName := fmt.Sprintf("vol-%s", rand.SafeEncodeString(fmt.Sprintf("%x", crc32.ChecksumIEEE([]byte(saVolume.MountPath)))))
	volume := corev1.Volume{
		Name: volName,
	}
	if saVolume.ConfigMap != nil {
		configMapVolumeSource := corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: saVolume.ConfigMap.Name},
			Items:                saVolume.ConfigMap.Items,
			DefaultMode:          saVolume.ConfigMap.DefaultMode,
			Optional:             saVolume.ConfigMap.Optional,
		}
		volume.ConfigMap = &configMapVolumeSource
	} else if saVolume.EmptyDir != nil {
		emptyDirVolumeSource := corev1.EmptyDirVolumeSource{
			Medium:    saVolume.EmptyDir.Medium,
			SizeLimit: saVolume.EmptyDir.SizeLimit,
		}
		volume.EmptyDir = &emptyDirVolumeSource
	} else if saVolume.PersistentVolumeClaim != nil {
		persistentVolumeClaimVolumeSource := corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: saVolume.PersistentVolumeClaim.ClaimName,
		}
		if saVolume.PersistentVolumeClaim.ReadOnly != nil {
			persistentVolumeClaimVolumeSource.ReadOnly = *saVolume.PersistentVolumeClaim.ReadOnly
		}
		volume.PersistentVolumeClaim = &persistentVolumeClaimVolumeSource
	} else if saVolume.Secret != nil {
		secretVolumeSource := corev1.SecretVolumeSource{
			SecretName:  saVolume.Secret.Name,
			Items:       saVolume.Secret.Items,
			DefaultMode: saVolume.Secret.DefaultMode,
			Optional:    saVolume.Secret.Optional,
		}
		volume.Secret = &secretVolumeSource
	} else if saVolume.CSI != nil {
		volume.CSI = saVolume.CSI
	} else {
		return corev1.Volume{}, corev1.VolumeMount{}, fmt.Errorf("volume for path %v in %v.%v does not have type", saVolume.MountPath, sa.Metadata.Namespace, sa.Metadata.Name)
	}
	volumeMount := corev1.VolumeMount{
		Name:      volName,
		MountPath: saVolume.MountPath,
	}
	return volume, volumeMount, nil
}

func (sa SimpleApp) delete(clientset *kubernetes.Clientset) error {
	// Get current Deployment
	oldDeployment, err := clientset.AppsV1().Deployments(sa.Metadata.Namespace).Get(context.TODO(), sa.Metadata.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		log.Printf("Deployment %v.%v already deleted", sa.Metadata.Namespace, sa.Metadata.Name)
	} else if err != nil {
		return err
	}
	managedBy, ok := oldDeployment.Labels[managedByLabel]
	if !ok || managedBy != managedByValue {
		return fmt.Errorf("found Deployment %v.%v not managed by us", oldDeployment.ObjectMeta.Namespace, oldDeployment.ObjectMeta.Name)
	}
	err = clientset.AppsV1().Deployments(oldDeployment.ObjectMeta.Namespace).Delete(context.TODO(), oldDeployment.ObjectMeta.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	log.Printf("Deleted Deployment %v.%v", sa.Metadata.Namespace, sa.Metadata.Name)

	// Get current Service
	oldService, err := clientset.CoreV1().Services(sa.Metadata.Namespace).Get(context.TODO(), sa.Metadata.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		log.Printf("Service %v.%v already deleted", sa.Metadata.Namespace, sa.Metadata.Name)
	} else if err != nil {
		return err
	}
	managedBy, ok = oldService.Labels[managedByLabel]
	if !ok || managedBy != managedByValue {
		return fmt.Errorf("found Service %v.%v not managed by us", oldService.ObjectMeta.Namespace, oldService.ObjectMeta.Name)
	}
	err = clientset.CoreV1().Services(oldService.ObjectMeta.Namespace).Delete(context.TODO(), oldService.ObjectMeta.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	log.Printf("Deleted Service %v.%v", oldService.ObjectMeta.Namespace, oldService.ObjectMeta.Name)
	return nil
}

func (sa *SimpleApp) fixPorts() bool {
	newPorts := make([]simpleAppPort, 0)
outer:
	for _, port := range sa.Spec.Ports {
		for _, stored := range newPorts {
			if port.HostPort == stored.HostPort && port.Protocol == stored.Protocol {
				continue outer
			}
		}
		newPorts = append(newPorts, port)
	}

	if len(sa.Spec.Ports) == len(newPorts) {
		return false
	}

	log.Printf("Removing %v duplicate port(s) from SimpleApp %v.%v", len(sa.Spec.Ports)-len(newPorts), sa.Metadata.Namespace, sa.Metadata.Name)
	sa.Spec.Ports = newPorts

	return true
}

func (sa *SimpleApp) fixVolumes() bool {
	newVolumes := make([]simpleAppVolume, 0)
outer:
	for _, volume := range sa.Spec.Volumes {
		for _, stored := range newVolumes {
			if volume.MountPath == stored.MountPath {
				continue outer
			}
		}
		newVolumes = append(newVolumes, volume)
	}

	if len(sa.Spec.Volumes) == len(newVolumes) {
		return false
	}

	log.Printf("Removing %v duplicate volume(s) from SimpleApp %v.%v", len(sa.Spec.Volumes)-len(newVolumes), sa.Metadata.Namespace, sa.Metadata.Name)
	sa.Spec.Volumes = newVolumes

	return true
}

func (sa *SimpleApp) updateApp(clientset *kubernetes.Clientset) error {
	payload, err := json.Marshal(sa)
	if err != nil {
		return err
	}

	result := clientset.RESTClient().Put().AbsPath("/apis/" + resourcePath).Namespace(sa.Metadata.Namespace).Resource(plural).Name(sa.Metadata.Name).Body(payload).Do(context.TODO())
	if result.Error() != nil {
		return result.Error()
	}
	return nil
}
