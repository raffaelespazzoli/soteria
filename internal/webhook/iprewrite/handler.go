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
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	// IPRewriteLabel is the label that triggers init container injection.
	IPRewriteLabel = "soteria.io/ip-rewrite"

	// MigrationJobLabel is set by KubeVirt on migration target pods.
	// Pods with this label must NOT have their disks modified.
	MigrationJobLabel = "kubevirt.io/migrationJobLabel"

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
func (h *Handler) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)

	pod := &corev1.Pod{}
	if err := json.Unmarshal(req.Object.Raw, pod); err != nil {
		logger.Error(err, "Failed to unmarshal Pod")
		return admission.Errored(http.StatusBadRequest, err)
	}

	logger = logger.WithValues("pod", pod.Name, "namespace", req.Namespace)

	// Migration pods must not have their disks modified.
	if _, isMigration := pod.Labels[MigrationJobLabel]; isMigration {
		logger.Info("Skipping injection for migration pod")
		return admission.Allowed("")
	}

	envVars := annotationsToEnvVars(pod.Annotations)
	if len(envVars) == 0 {
		logger.Info("No IP rewrite annotations found, skipping injection")
		return admission.Allowed("")
	}

	volumeMounts := pvcVolumeMounts(pod.Spec.Volumes)

	initContainer := corev1.Container{
		Name:         initContainerName,
		Image:        h.InitContainerImage,
		Env:          envVars,
		VolumeMounts: volumeMounts,
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"SYS_ADMIN"},
			},
		},
	}

	// Prepend — the IP rewrite init container must run before all others.
	pod.Spec.InitContainers = append([]corev1.Container{initContainer}, pod.Spec.InitContainers...)

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		logger.Error(err, "Failed to marshal mutated Pod")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	logger.Info("Injected IP rewrite init container",
		"image", h.InitContainerImage,
		"envVars", len(envVars),
		"volumeMounts", len(volumeMounts))

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

// annotationsToEnvVars converts Soteria IP/DNS annotations to environment
// variables for the init container.
//
// Transformation rules:
//   - soteria.io/dns          → SOTERIA_DNS
//   - soteria.io/<name>-ip    → SOTERIA_<NAME>_IP  (e.g. eth0-ip → SOTERIA_ETH0_IP)
//   - Other soteria.io/* annotations are ignored (e.g. soteria.io/drplan)
//   - Non-soteria.io annotations are ignored
func annotationsToEnvVars(annotations map[string]string) []corev1.EnvVar {
	var envVars []corev1.EnvVar

	for key, value := range annotations {
		if !strings.HasPrefix(key, soteriaAnnotationPrefix) {
			continue
		}

		suffix := key[len(soteriaAnnotationPrefix):]

		switch {
		case suffix == dnsSuffix:
			envVars = append(envVars, corev1.EnvVar{
				Name:  "SOTERIA_DNS",
				Value: value,
			})

		case strings.HasSuffix(suffix, ipSuffix):
			// Strip trailing "-ip", uppercase, replace "-" with "_", wrap with SOTERIA_ prefix and _IP suffix.
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

// pvcVolumeMounts returns VolumeMount entries for all PVC-backed volumes in
// the pod spec. Each PVC volume is mounted at /disks/<volumeName>.
func pvcVolumeMounts(volumes []corev1.Volume) []corev1.VolumeMount {
	var mounts []corev1.VolumeMount

	for _, vol := range volumes {
		if vol.PersistentVolumeClaim != nil {
			mounts = append(mounts, corev1.VolumeMount{
				Name:      vol.Name,
				MountPath: diskMountPrefix + vol.Name,
			})
		}
	}

	return mounts
}
