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

package iprewrite

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	testVolumeName = "rootdisk"
	testDiskPath   = "/disks/rootdisk"
)

// makePodRequest marshals a Pod and wraps it in a CREATE admission.Request.
func makePodRequest(pod *corev1.Pod) admission.Request {
	raw, _ := json.Marshal(pod)
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

// mustApplyPatches applies the JSON patches from the admission response to
// the original pod and returns the mutated result. It uses the raw Patch
// field from the AdmissionResponse rather than the structured Patches list.
func mustApplyPatches(t *testing.T, original *corev1.Pod, resp admission.Response) *corev1.Pod {
	t.Helper()

	if resp.Patches == nil {
		return original.DeepCopy()
	}

	rawOriginal, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal original: %v", err)
	}

	patchBytes, err := json.Marshal(resp.Patches)
	if err != nil {
		t.Fatalf("marshal patches: %v", err)
	}

	patched, err := applyJSONPatch(rawOriginal, patchBytes)
	if err != nil {
		t.Fatalf("apply patch: %v", err)
	}

	result := &corev1.Pod{}
	if err := json.Unmarshal(patched, result); err != nil {
		t.Fatalf("unmarshal patched pod: %v", err)
	}
	return result
}

// findIPRewriteInitContainer returns the ip-rewrite init container, or nil.
func findIPRewriteInitContainer(pod *corev1.Pod) *corev1.Container {
	for i := range pod.Spec.InitContainers {
		if pod.Spec.InitContainers[i].Name == initContainerName {
			return &pod.Spec.InitContainers[i]
		}
	}
	return nil
}

// findEnvVar returns the env var with the given name, or nil.
func findEnvVar(envVars []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range envVars {
		if envVars[i].Name == name {
			return &envVars[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tests: Handler.Handle — init container injection
// ---------------------------------------------------------------------------

func TestHandle_LabelAndAnnotations_InjectsInitContainer(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-vm1",
			Namespace: "default",
			Labels: map[string]string{
				IPRewriteLabel: "true",
			},
			Annotations: map[string]string{
				"soteria.io/eth0-ip": "10.0.2.100/24;10.0.2.1",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "compute", Image: "registry.kubevirt.io/virt-launcher:v1.0.0"},
			},
			Volumes: []corev1.Volume{
				{
					Name: "rootdisk",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "vm1-rootdisk",
						},
					},
				},
			},
		},
	}

	handler := &Handler{InitContainerImage: "test-image:v1"}
	resp := handler.Handle(context.Background(), makePodRequest(pod))

	if !resp.Allowed {
		t.Fatalf("expected Allowed=true, got false: %v", resp.Result)
	}
	if resp.Patches == nil {
		t.Fatal("expected patches but got nil")
	}

	mutated := mustApplyPatches(t, pod, resp)

	ic := findIPRewriteInitContainer(mutated)
	if ic == nil {
		t.Fatal("init container not found in mutated pod")
	}

	if ic.Image != "test-image:v1" {
		t.Errorf("init container image = %q, want %q", ic.Image, "test-image:v1")
	}

	ev := findEnvVar(ic.Env, "SOTERIA_ETH0_IP")
	if ev == nil {
		t.Fatal("SOTERIA_ETH0_IP env var not found")
	}
	if ev.Value != "10.0.2.100/24;10.0.2.1" {
		t.Errorf("SOTERIA_ETH0_IP = %q, want %q", ev.Value, "10.0.2.100/24;10.0.2.1")
	}

	// Verify SYS_ADMIN capability
	if ic.SecurityContext == nil || ic.SecurityContext.Capabilities == nil {
		t.Fatal("expected security context with capabilities")
	}
	hasSysAdmin := slices.Contains(ic.SecurityContext.Capabilities.Add, "SYS_ADMIN")
	if !hasSysAdmin {
		t.Error("SYS_ADMIN capability not found")
	}

	// Verify PVC volume mount
	if len(ic.VolumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount, got %d", len(ic.VolumeMounts))
	}
	if ic.VolumeMounts[0].Name != testVolumeName {
		t.Errorf("volume mount name = %q, want %q", ic.VolumeMounts[0].Name, testVolumeName)
	}
	if ic.VolumeMounts[0].MountPath != testDiskPath {
		t.Errorf("volume mount path = %q, want %q", ic.VolumeMounts[0].MountPath, testDiskPath)
	}
}

func TestHandle_MigrationPod_SkipsInjection(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-migration",
			Namespace: "default",
			Labels: map[string]string{
				IPRewriteLabel:                "true",
				"kubevirt.io/migrationJobUID": "abc123",
			},
			Annotations: map[string]string{
				"soteria.io/eth0-ip": "10.0.2.100/24;10.0.2.1",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "compute", Image: "registry.kubevirt.io/virt-launcher:v1.0.0"},
			},
		},
	}

	handler := &Handler{InitContainerImage: "test-image:v1"}
	resp := handler.Handle(context.Background(), makePodRequest(pod))

	if !resp.Allowed {
		t.Fatalf("expected Allowed=true, got false: %v", resp.Result)
	}
	if resp.Patches != nil {
		t.Errorf("expected no patches for migration pod, got %d patches", len(resp.Patches))
	}
}

func TestHandle_NoIPAnnotations_SkipsInjection(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-noannot",
			Namespace: "default",
			Labels: map[string]string{
				IPRewriteLabel: "true",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "compute", Image: "registry.kubevirt.io/virt-launcher:v1.0.0"},
			},
		},
	}

	handler := &Handler{InitContainerImage: "test-image:v1"}
	resp := handler.Handle(context.Background(), makePodRequest(pod))

	if !resp.Allowed {
		t.Fatalf("expected Allowed=true, got false: %v", resp.Result)
	}
	if resp.Patches != nil {
		t.Errorf("expected no patches (no IP annotations), got %d patches", len(resp.Patches))
	}
}

func TestHandle_LabelOnly_NonIPAnnotations_NoInjection(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-label-only",
			Namespace: "default",
			Labels: map[string]string{
				IPRewriteLabel: "true",
			},
			Annotations: map[string]string{
				"soteria.io/ip-rewrite": "true",
				"soteria.io/drplan":     "plan-erp",
				"some-other/annotation": "value",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "compute", Image: "registry.kubevirt.io/virt-launcher:v1.0.0"},
			},
		},
	}

	handler := &Handler{InitContainerImage: "test-image:v1"}
	resp := handler.Handle(context.Background(), makePodRequest(pod))

	if !resp.Allowed {
		t.Fatalf("expected Allowed=true, got false: %v", resp.Result)
	}
	if resp.Patches != nil {
		t.Errorf("expected no patches (no *-ip or dns annotations), got %d patches", len(resp.Patches))
	}
}

func TestHandle_MultiNIC_TwoIPAnnotations(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-multinic",
			Namespace: "default",
			Labels: map[string]string{
				IPRewriteLabel: "true",
			},
			Annotations: map[string]string{
				"soteria.io/eth0-ip": "10.0.2.100/24;10.0.2.1",
				"soteria.io/ens3-ip": "192.168.1.50/16;192.168.0.1",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "compute", Image: "registry.kubevirt.io/virt-launcher:v1.0.0"},
			},
			Volumes: []corev1.Volume{
				{
					Name: "rootdisk",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "vm1-rootdisk",
						},
					},
				},
			},
		},
	}

	handler := &Handler{InitContainerImage: "test-image:v1"}
	resp := handler.Handle(context.Background(), makePodRequest(pod))

	if !resp.Allowed {
		t.Fatalf("expected Allowed=true, got false: %v", resp.Result)
	}

	mutated := mustApplyPatches(t, pod, resp)

	ic := findIPRewriteInitContainer(mutated)
	if ic == nil {
		t.Fatal("init container not found")
	}

	if len(ic.Env) != 2 {
		t.Fatalf("expected 2 env vars, got %d: %+v", len(ic.Env), ic.Env)
	}

	ens3 := findEnvVar(ic.Env, "SOTERIA_ENS3_IP")
	if ens3 == nil {
		t.Fatal("SOTERIA_ENS3_IP env var not found")
	}
	if ens3.Value != "192.168.1.50/16;192.168.0.1" {
		t.Errorf("SOTERIA_ENS3_IP = %q, want %q", ens3.Value, "192.168.1.50/16;192.168.0.1")
	}

	eth0 := findEnvVar(ic.Env, "SOTERIA_ETH0_IP")
	if eth0 == nil {
		t.Fatal("SOTERIA_ETH0_IP env var not found")
	}
}

func TestHandle_DNSAnnotation_SetsEnvVar(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-dns",
			Namespace: "default",
			Labels: map[string]string{
				IPRewriteLabel: "true",
			},
			Annotations: map[string]string{
				"soteria.io/eth0-ip": "10.0.2.100/24;10.0.2.1",
				"soteria.io/dns":     "10.0.2.10,10.0.2.11",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "compute", Image: "registry.kubevirt.io/virt-launcher:v1.0.0"},
			},
			Volumes: []corev1.Volume{
				{
					Name: "rootdisk",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "vm1-rootdisk",
						},
					},
				},
			},
		},
	}

	handler := &Handler{InitContainerImage: "test-image:v1"}
	resp := handler.Handle(context.Background(), makePodRequest(pod))

	if !resp.Allowed {
		t.Fatalf("expected Allowed=true, got false: %v", resp.Result)
	}

	mutated := mustApplyPatches(t, pod, resp)

	ic := findIPRewriteInitContainer(mutated)
	if ic == nil {
		t.Fatal("init container not found")
	}

	dns := findEnvVar(ic.Env, "SOTERIA_DNS")
	if dns == nil {
		t.Fatal("SOTERIA_DNS env var not found")
	}
	if dns.Value != "10.0.2.10,10.0.2.11" {
		t.Errorf("SOTERIA_DNS = %q, want %q", dns.Value, "10.0.2.10,10.0.2.11")
	}
}

func TestHandle_PVCVolumeMounts_InjectedIntoInitContainer(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-pvcs",
			Namespace: "default",
			Annotations: map[string]string{
				"soteria.io/eth0-ip": "10.0.2.100/24;10.0.2.1",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "compute", Image: "registry.kubevirt.io/virt-launcher:v1.0.0"},
			},
			Volumes: []corev1.Volume{
				{
					Name: "rootdisk",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "vm1-rootdisk",
						},
					},
				},
				{
					Name: "datadisk",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "vm1-datadisk",
						},
					},
				},
			},
		},
	}

	handler := &Handler{InitContainerImage: "test-image:v1"}
	resp := handler.Handle(context.Background(), makePodRequest(pod))

	mutated := mustApplyPatches(t, pod, resp)

	ic := findIPRewriteInitContainer(mutated)
	if ic == nil {
		t.Fatal("init container not found")
	}

	if len(ic.VolumeMounts) != 2 {
		t.Fatalf("expected 2 volume mounts, got %d", len(ic.VolumeMounts))
	}

	mountNames := make(map[string]string)
	for _, vm := range ic.VolumeMounts {
		mountNames[vm.Name] = vm.MountPath
	}
	if mountNames[testVolumeName] != testDiskPath {
		t.Errorf("rootdisk mount path = %q, want %s", mountNames[testVolumeName], testDiskPath)
	}
	if mountNames["datadisk"] != "/disks/datadisk" {
		t.Errorf("datadisk mount path = %q, want /disks/datadisk", mountNames["datadisk"])
	}
}

func TestHandle_NonPVCVolumes_NotMountedInInitContainer(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-mixed-volumes",
			Namespace: "default",
			Annotations: map[string]string{
				"soteria.io/eth0-ip": "10.0.2.100/24;10.0.2.1",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "compute", Image: "registry.kubevirt.io/virt-launcher:v1.0.0"},
			},
			Volumes: []corev1.Volume{
				{
					Name: "rootdisk",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "vm1-rootdisk",
						},
					},
				},
				{
					Name: "cloudinit",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "vm1-cloudinit"},
						},
					},
				},
				{
					Name: "serviceaccount",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: "default-token"},
					},
				},
			},
		},
	}

	handler := &Handler{InitContainerImage: "test-image:v1"}
	resp := handler.Handle(context.Background(), makePodRequest(pod))

	mutated := mustApplyPatches(t, pod, resp)

	ic := findIPRewriteInitContainer(mutated)
	if ic == nil {
		t.Fatal("init container not found")
	}

	if len(ic.VolumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount (only PVC), got %d: %+v", len(ic.VolumeMounts), ic.VolumeMounts)
	}
	if ic.VolumeMounts[0].Name != testVolumeName {
		t.Errorf("volume mount name = %q, want %s", ic.VolumeMounts[0].Name, testVolumeName)
	}
}

func TestHandle_InitContainerPrepended(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-prepend",
			Namespace: "default",
			Annotations: map[string]string{
				"soteria.io/eth0-ip": "10.0.2.100/24;10.0.2.1",
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "existing-init", Image: "busybox:latest"},
			},
			Containers: []corev1.Container{
				{Name: "compute", Image: "registry.kubevirt.io/virt-launcher:v1.0.0"},
			},
			Volumes: []corev1.Volume{
				{
					Name: "rootdisk",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "vm1-rootdisk",
						},
					},
				},
			},
		},
	}

	handler := &Handler{InitContainerImage: "test-image:v1"}
	resp := handler.Handle(context.Background(), makePodRequest(pod))

	mutated := mustApplyPatches(t, pod, resp)

	if len(mutated.Spec.InitContainers) != 2 {
		t.Fatalf("expected 2 init containers, got %d", len(mutated.Spec.InitContainers))
	}
	if mutated.Spec.InitContainers[0].Name != initContainerName {
		t.Errorf("first init container = %q, want %q (prepended)", mutated.Spec.InitContainers[0].Name, initContainerName)
	}
	if mutated.Spec.InitContainers[1].Name != "existing-init" {
		t.Errorf("second init container = %q, want existing-init", mutated.Spec.InitContainers[1].Name)
	}
}

func TestHandle_AlreadyInjected_SkipsDoubleInjection(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-already",
			Namespace: "default",
			Annotations: map[string]string{
				"soteria.io/eth0-ip": "10.0.2.100/24;10.0.2.1",
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: initContainerName, Image: "old-image:v0"},
			},
			Containers: []corev1.Container{
				{Name: "compute", Image: "registry.kubevirt.io/virt-launcher:v1.0.0"},
			},
		},
	}

	handler := &Handler{InitContainerImage: "test-image:v1"}
	resp := handler.Handle(context.Background(), makePodRequest(pod))

	if !resp.Allowed {
		t.Fatalf("expected Allowed=true, got false")
	}
	if resp.Patches != nil {
		t.Errorf("expected no patches (already injected), got %d", len(resp.Patches))
	}
}

func TestHandle_DefaultImage_UsedWhenNotConfigured(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-default-image",
			Namespace: "default",
			Annotations: map[string]string{
				"soteria.io/eth0-ip": "10.0.2.100/24;10.0.2.1",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "compute", Image: "registry.kubevirt.io/virt-launcher:v1.0.0"},
			},
			Volumes: []corev1.Volume{
				{
					Name: "rootdisk",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "vm1-rootdisk",
						},
					},
				},
			},
		},
	}

	handler := &Handler{}
	resp := handler.Handle(context.Background(), makePodRequest(pod))

	mutated := mustApplyPatches(t, pod, resp)
	ic := findIPRewriteInitContainer(mutated)
	if ic == nil {
		t.Fatal("init container not found")
	}
	if ic.Image != DefaultInitContainerImage {
		t.Errorf("init container image = %q, want default %q", ic.Image, DefaultInitContainerImage)
	}
}

func TestHandle_BlockModeVolume_VolumeDeviceInjected(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-block",
			Namespace: "default",
			Annotations: map[string]string{
				"soteria.io/eth0-ip": "10.0.2.100/24;10.0.2.1",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "compute",
					Image: "registry.kubevirt.io/virt-launcher:v1.0.0",
					VolumeDevices: []corev1.VolumeDevice{
						{Name: "rootdisk", DevicePath: "/dev/rootdisk"},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "rootdisk",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "vm1-rootdisk",
						},
					},
				},
			},
		},
	}

	handler := &Handler{InitContainerImage: "test-image:v1"}
	resp := handler.Handle(context.Background(), makePodRequest(pod))

	mutated := mustApplyPatches(t, pod, resp)
	ic := findIPRewriteInitContainer(mutated)
	if ic == nil {
		t.Fatal("init container not found")
	}

	if len(ic.VolumeMounts) != 0 {
		t.Errorf("expected 0 volume mounts for block volume, got %d", len(ic.VolumeMounts))
	}
	if len(ic.VolumeDevices) != 1 {
		t.Fatalf("expected 1 volume device, got %d", len(ic.VolumeDevices))
	}
	if ic.VolumeDevices[0].Name != testVolumeName {
		t.Errorf("volume device name = %q, want %s", ic.VolumeDevices[0].Name, testVolumeName)
	}
	if ic.VolumeDevices[0].DevicePath != testDiskPath {
		t.Errorf("volume device path = %q, want %s", ic.VolumeDevices[0].DevicePath, testDiskPath)
	}
}

func TestHandle_InvalidPodJSON_FailOpen(t *testing.T) {
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Name:      "bad-pod",
			Namespace: "default",
			Object:    runtime.RawExtension{Raw: []byte(`{"invalid json`)},
		},
	}

	handler := &Handler{InitContainerImage: "test-image:v1"}
	resp := handler.Handle(context.Background(), req)

	if !resp.Allowed {
		t.Error("expected fail-open (Allowed=true) on bad JSON")
	}
}

func TestHandle_SecurityContext(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-sec",
			Namespace: "default",
			Annotations: map[string]string{
				"soteria.io/eth0-ip": "10.0.2.100/24;10.0.2.1",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "compute", Image: "registry.kubevirt.io/virt-launcher:v1.0.0"},
			},
			Volumes: []corev1.Volume{
				{
					Name: "rootdisk",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "vm1-rootdisk",
						},
					},
				},
			},
		},
	}

	handler := &Handler{InitContainerImage: "test-image:v1"}
	resp := handler.Handle(context.Background(), makePodRequest(pod))

	mutated := mustApplyPatches(t, pod, resp)
	ic := findIPRewriteInitContainer(mutated)
	if ic == nil {
		t.Fatal("init container not found")
	}

	sc := ic.SecurityContext
	if sc == nil {
		t.Fatal("expected non-nil security context")
	}
	if sc.RunAsUser == nil || *sc.RunAsUser != 0 {
		t.Errorf("RunAsUser = %v, want 0", sc.RunAsUser)
	}
	if sc.RunAsNonRoot == nil || *sc.RunAsNonRoot != false {
		t.Errorf("RunAsNonRoot = %v, want false", sc.RunAsNonRoot)
	}
	if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation != true {
		t.Errorf("AllowPrivilegeEscalation = %v, want true", sc.AllowPrivilegeEscalation)
	}
}

// ---------------------------------------------------------------------------
// Tests: annotationsToEnvVars (table-driven)
// ---------------------------------------------------------------------------

func TestAnnotationsToEnvVars(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		want        []corev1.EnvVar
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			want:        nil,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			want:        nil,
		},
		{
			name: "eth0-ip annotation",
			annotations: map[string]string{
				"soteria.io/eth0-ip": "10.0.2.100/24;10.0.2.1",
			},
			want: []corev1.EnvVar{
				{Name: "SOTERIA_ETH0_IP", Value: "10.0.2.100/24;10.0.2.1"},
			},
		},
		{
			name: "ens3-ip annotation",
			annotations: map[string]string{
				"soteria.io/ens3-ip": "192.168.1.50/16;192.168.0.1",
			},
			want: []corev1.EnvVar{
				{Name: "SOTERIA_ENS3_IP", Value: "192.168.1.50/16;192.168.0.1"},
			},
		},
		{
			name: "custom NIC name with hyphens",
			annotations: map[string]string{
				"soteria.io/my-custom-nic-ip": "172.16.0.5/20;172.16.0.1",
			},
			want: []corev1.EnvVar{
				{Name: "SOTERIA_MY_CUSTOM_NIC_IP", Value: "172.16.0.5/20;172.16.0.1"},
			},
		},
		{
			name: "dns annotation",
			annotations: map[string]string{
				"soteria.io/dns": "10.0.2.10,10.0.2.11",
			},
			want: []corev1.EnvVar{
				{Name: "SOTERIA_DNS", Value: "10.0.2.10,10.0.2.11"},
			},
		},
		{
			name: "non-soteria annotations ignored",
			annotations: map[string]string{
				"kubernetes.io/name":    "test",
				"app.kubernetes.io/ver": "v1",
			},
			want: nil,
		},
		{
			name: "soteria.io/ip-rewrite label-key ignored",
			annotations: map[string]string{
				"soteria.io/ip-rewrite": "true",
			},
			want: nil,
		},
		{
			name: "malformed annotations without -ip suffix ignored",
			annotations: map[string]string{
				"soteria.io/something":  "value",
				"soteria.io/other-data": "data",
				"soteria.io/drplan":     "plan-erp",
			},
			want: nil,
		},
		{
			name: "mixed valid and invalid annotations sorted",
			annotations: map[string]string{
				"soteria.io/eth0-ip":    "10.0.2.100/24;10.0.2.1",
				"soteria.io/dns":        "8.8.8.8",
				"soteria.io/ip-rewrite": "true",
				"soteria.io/drplan":     "plan-erp",
				"kubernetes.io/name":    "test",
			},
			want: []corev1.EnvVar{
				{Name: "SOTERIA_DNS", Value: "8.8.8.8"},
				{Name: "SOTERIA_ETH0_IP", Value: "10.0.2.100/24;10.0.2.1"},
			},
		},
		{
			name: "multiple IPs sorted deterministically",
			annotations: map[string]string{
				"soteria.io/ens3-ip": "192.168.1.50/16;192.168.0.1",
				"soteria.io/eth0-ip": "10.0.2.100/24;10.0.2.1",
			},
			want: []corev1.EnvVar{
				{Name: "SOTERIA_ENS3_IP", Value: "192.168.1.50/16;192.168.0.1"},
				{Name: "SOTERIA_ETH0_IP", Value: "10.0.2.100/24;10.0.2.1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := annotationsToEnvVars(tt.annotations)

			if len(got) != len(tt.want) {
				t.Fatalf("len(envVars) = %d, want %d\ngot:  %+v\nwant: %+v", len(got), len(tt.want), got, tt.want)
			}

			for i := range got {
				if got[i].Name != tt.want[i].Name {
					t.Errorf("envVars[%d].Name = %q, want %q", i, got[i].Name, tt.want[i].Name)
				}
				if got[i].Value != tt.want[i].Value {
					t.Errorf("envVars[%d].Value = %q, want %q", i, got[i].Value, tt.want[i].Value)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: pvcVolumeAccess
// ---------------------------------------------------------------------------

func TestPVCVolumeAccess(t *testing.T) {
	volumes := []corev1.Volume{
		{
			Name: "rootdisk",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "vm-rootdisk",
				},
			},
		},
		{
			Name: "cloudinit",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "cloudinit"},
				},
			},
		},
		{
			Name: "datadisk",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "vm-datadisk",
				},
			},
		},
	}

	blockVols := map[string]bool{"datadisk": true}

	mounts, devices := pvcVolumeAccess(volumes, blockVols)

	if len(mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(mounts))
	}
	if mounts[0].Name != testVolumeName {
		t.Errorf("mount name = %q, want %s", mounts[0].Name, testVolumeName)
	}
	if mounts[0].MountPath != testDiskPath {
		t.Errorf("mount path = %q, want %s", mounts[0].MountPath, testDiskPath)
	}

	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].Name != "datadisk" {
		t.Errorf("device name = %q, want datadisk", devices[0].Name)
	}
	if devices[0].DevicePath != "/disks/datadisk" {
		t.Errorf("device path = %q, want /disks/datadisk", devices[0].DevicePath)
	}
}

// ---------------------------------------------------------------------------
// Tests: blockModeVolumes
// ---------------------------------------------------------------------------

func TestBlockModeVolumes(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "compute",
					VolumeDevices: []corev1.VolumeDevice{
						{Name: "rootdisk", DevicePath: "/dev/rootdisk"},
					},
				},
			},
			InitContainers: []corev1.Container{
				{
					Name: "init",
					VolumeDevices: []corev1.VolumeDevice{
						{Name: "datadisk", DevicePath: "/dev/datadisk"},
					},
				},
			},
		},
	}

	blocks := blockModeVolumes(pod)

	if !blocks[testVolumeName] {
		t.Error("expected rootdisk in block volumes")
	}
	if !blocks["datadisk"] {
		t.Error("expected datadisk in block volumes")
	}
	if blocks["nonexistent"] {
		t.Error("nonexistent should not be in block volumes")
	}
}

// ---------------------------------------------------------------------------
// JSON Patch helpers — minimal RFC 6902 implementation for test verification
// ---------------------------------------------------------------------------

// applyJSONPatch applies RFC 6902 JSON Patch operations to a JSON document.
func applyJSONPatch(doc, patchJSON []byte) ([]byte, error) {
	var ops []struct {
		Op    string          `json:"op"`
		Path  string          `json:"path"`
		Value json.RawMessage `json:"value,omitempty"`
	}
	if err := json.Unmarshal(patchJSON, &ops); err != nil {
		return nil, err
	}

	var root any
	if err := json.Unmarshal(doc, &root); err != nil {
		return nil, err
	}

	for _, op := range ops {
		var val any
		if op.Value != nil {
			_ = json.Unmarshal(op.Value, &val)
		}

		parts := splitJSONPointer(op.Path)

		switch op.Op {
		case "add":
			root = setNested(root, parts, val, true)
		case "replace":
			root = setNested(root, parts, val, false)
		case "remove":
			root = removeNested(root, parts)
		}
	}

	return json.Marshal(root)
}

func splitJSONPointer(path string) []string {
	if path == "" || path == "/" {
		return nil
	}
	raw := path[1:] // strip leading "/"
	var parts []string
	for _, seg := range splitOnSlash(raw) {
		seg = unescapePointer(seg)
		parts = append(parts, seg)
	}
	return parts
}

func splitOnSlash(s string) []string {
	var result []string
	current := ""
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			result = append(result, current)
			current = ""
		} else {
			current += string(s[i])
		}
	}
	return append(result, current)
}

func unescapePointer(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && s[i] == '~' {
			if s[i+1] == '1' {
				out.WriteString("/")
				i++
				continue
			}
			if s[i+1] == '0' {
				out.WriteString("~")
				i++
				continue
			}
		}
		out.WriteString(string(s[i]))
	}
	return out.String()
}

func setNested(doc any, parts []string, val any, isAdd bool) any {
	if len(parts) == 0 {
		return val
	}

	key := parts[0]

	if len(parts) == 1 {
		switch d := doc.(type) {
		case map[string]any:
			d[key] = val
			return d
		case []any:
			if key == "-" {
				return append(d, val)
			}
			idx := mustAtoi(key)
			if isAdd {
				result := make([]any, 0, len(d)+1)
				result = append(result, d[:idx]...)
				result = append(result, val)
				result = append(result, d[idx:]...)
				return result
			}
			d[idx] = val
			return d
		case nil:
			return map[string]any{key: val}
		}
		return doc
	}

	switch d := doc.(type) {
	case map[string]any:
		child, ok := d[key]
		if !ok {
			child = map[string]any{}
		}
		d[key] = setNested(child, parts[1:], val, isAdd)
		return d
	case []any:
		idx := mustAtoi(key)
		if idx < len(d) {
			d[idx] = setNested(d[idx], parts[1:], val, isAdd)
		}
		return d
	}
	return doc
}

func removeNested(doc any, parts []string) any {
	if len(parts) == 0 {
		return nil
	}

	key := parts[0]

	if len(parts) == 1 {
		switch d := doc.(type) {
		case map[string]any:
			delete(d, key)
			return d
		case []any:
			idx := mustAtoi(key)
			return append(d[:idx], d[idx+1:]...)
		}
		return doc
	}

	switch d := doc.(type) {
	case map[string]any:
		if child, ok := d[key]; ok {
			d[key] = removeNested(child, parts[1:])
		}
		return d
	case []any:
		idx := mustAtoi(key)
		if idx < len(d) {
			d[idx] = removeNested(d[idx], parts[1:])
		}
		return d
	}
	return doc
}

func mustAtoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
