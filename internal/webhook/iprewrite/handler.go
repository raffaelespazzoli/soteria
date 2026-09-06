/*
Copyright 2026 The Soteria Authors.

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

// Package iprewrite implements a mutating admission webhook that injects an
// IP rewrite init container into virt-launcher pods. The init container
// modifies VM disk images with the correct network configuration before the
// VM boots.
package iprewrite

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	virtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	// IPRewriteLabel is the label that triggers init container injection.
	IPRewriteLabel = "soteria.io/ip-rewrite"

	// soteriaAnnotationPrefix is the prefix for all Soteria annotations.
	soteriaAnnotationPrefix = "soteria.io/"

	// dnsSuffix identifies the DNS annotation.
	dnsSuffix = "dns"

	// ipSuffix identifies IP-address annotations.
	ipSuffix = "-ip"

	// initContainerName is the name of the injected init container.
	initContainerName = "ip-rewrite"

	// diskMountPrefix is the base path where PVC volumes are mounted.
	diskMountPrefix = "/disks/"

	// diskDevicePrefix is the base path where block-mode PVC volumes are exposed.
	diskDevicePrefix = "/disks/"

	// DefaultInitContainerImage is the default image for the IP rewrite init container.
	DefaultInitContainerImage = "quay.io/raffaelespazzoli/soteria-ip-rewrite:latest"

	// MutatePodPath is the webhook endpoint path.
	MutatePodPath = "/mutate-v1-pod"
)

// Handler is a mutating admission webhook handler that injects an IP rewrite
// init container into pods labelled with soteria.io/ip-rewrite=true.
//
// The handler is designed to be testable with pure admission.Request objects —
// no envtest or running cluster is needed.
type Handler struct {
	// InitContainerImage is the container image for the IP rewrite init
	// container. Configurable via --init-container-image flag.
	InitContainerImage string
}

// Handle processes a pod admission request. It injects an IP rewrite init
// container when the pod has the soteria.io/ip-rewrite label and is not a
// migration pod.
//
// On internal errors the handler returns admission.Allowed (fail-open) with a
// logged error, matching the MutatingWebhookConfiguration's failurePolicy: Ignore.
func (h *Handler) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)

	pod := &corev1.Pod{}
	if err := json.Unmarshal(req.Object.Raw, pod); err != nil {
		logger.Error(err, "Failed to unmarshal Pod, admitting unmodified (fail-open)")
		return admission.Allowed("")
	}

	logger = logger.WithValues("pod", pod.Name, "namespace", req.Namespace)

	// Migration pods must not have their disks modified.
	// Uses the KubeVirt API constant (resolves to "kubevirt.io/migrationJobUID").
	if _, isMigration := pod.Labels[virtv1.MigrationJobLabel]; isMigration {
		logger.Info("Skipping injection for migration pod")
		return admission.Allowed("")
	}

	// Guard: skip if an ip-rewrite init container is already present.
	for _, c := range pod.Spec.InitContainers {
		if c.Name == initContainerName {
			logger.Info("Init container already present, skipping injection")
			return admission.Allowed("")
		}
	}

	envVars := annotationsToEnvVars(pod.Annotations)
	if len(envVars) == 0 {
		logger.Info("No IP rewrite annotations found, skipping injection")
		return admission.Allowed("")
	}

	image := h.InitContainerImage
	if image == "" {
		image = DefaultInitContainerImage
	}

	// Determine which PVC volumes use block mode vs filesystem mode by
	// inspecting existing containers' volumeDevices declarations.
	blockVolumes := blockModeVolumes(pod)
	volumeMounts, volumeDevices := pvcVolumeAccess(pod.Spec.Volumes, blockVolumes)

	initContainer := corev1.Container{
		Name:          initContainerName,
		Image:         image,
		Env:           envVars,
		VolumeMounts:  volumeMounts,
		VolumeDevices: volumeDevices,
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                new(int64(0)),
			RunAsNonRoot:             new(false),
			AllowPrivilegeEscalation: new(true),
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"SYS_ADMIN"},
			},
		},
	}

	// Prepend — the IP rewrite init container must run before all others.
	pod.Spec.InitContainers = append([]corev1.Container{initContainer}, pod.Spec.InitContainers...)

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		logger.Error(err, "Failed to marshal mutated Pod, admitting unmodified (fail-open)")
		return admission.Allowed("")
	}

	logger.Info("Injected IP rewrite init container",
		"image", image,
		"envVars", len(envVars),
		"volumeMounts", len(volumeMounts),
		"volumeDevices", len(volumeDevices))

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

// annotationsToEnvVars converts Soteria IP/DNS annotations to environment
// variables for the init container. Keys are sorted for deterministic output.
//
// Transformation rules:
//   - soteria.io/dns          → SOTERIA_DNS
//   - soteria.io/<name>-ip    → SOTERIA_<NAME>_IP  (e.g. eth0-ip → SOTERIA_ETH0_IP)
//   - Other soteria.io/* annotations are ignored (e.g. soteria.io/drplan)
//   - Non-soteria.io annotations are ignored
func annotationsToEnvVars(annotations map[string]string) []corev1.EnvVar {
	// Collect and sort annotation keys for deterministic output.
	keys := make([]string, 0, len(annotations))
	for key := range annotations {
		if strings.HasPrefix(key, soteriaAnnotationPrefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var envVars []corev1.EnvVar

	for _, key := range keys {
		value := annotations[key]
		suffix := key[len(soteriaAnnotationPrefix):]

		switch {
		case suffix == dnsSuffix:
			envVars = append(envVars, corev1.EnvVar{
				Name:  "SOTERIA_DNS",
				Value: value,
			})

		case strings.HasSuffix(suffix, ipSuffix):
			name := suffix[:len(suffix)-len(ipSuffix)]
			envName := "SOTERIA_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_IP"
			envVars = append(envVars, corev1.EnvVar{
				Name:  envName,
				Value: value,
			})
		}
	}

	return envVars
}

// blockModeVolumes builds a set of volume names that are used as raw block
// devices by any container or init container in the pod. When a volume appears
// in volumeDevices, guestfish must receive it as a device path, not a mount.
func blockModeVolumes(pod *corev1.Pod) map[string]bool {
	blocks := make(map[string]bool)
	for _, c := range pod.Spec.Containers {
		for _, vd := range c.VolumeDevices {
			blocks[vd.Name] = true
		}
	}
	for _, c := range pod.Spec.InitContainers {
		for _, vd := range c.VolumeDevices {
			blocks[vd.Name] = true
		}
	}
	return blocks
}

// pvcVolumeAccess returns VolumeMount and VolumeDevice entries for all
// PVC-backed volumes. Block-mode volumes (identified by blockVolumes) are
// exposed as VolumeDevices; filesystem-mode volumes are mounted normally.
func pvcVolumeAccess(volumes []corev1.Volume, blockVolumes map[string]bool) ([]corev1.VolumeMount, []corev1.VolumeDevice) {
	var mounts []corev1.VolumeMount
	var devices []corev1.VolumeDevice

	for _, vol := range volumes {
		if vol.PersistentVolumeClaim == nil {
			continue
		}

		if blockVolumes[vol.Name] {
			devices = append(devices, corev1.VolumeDevice{
				Name:       vol.Name,
				DevicePath: diskDevicePrefix + vol.Name,
			})
		} else {
			mounts = append(mounts, corev1.VolumeMount{
				Name:      vol.Name,
				MountPath: diskMountPrefix + vol.Name,
			})
		}
	}

	return mounts, devices
}
