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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DRPlan Phase values — 8-phase symmetric lifecycle.
// Rest states (4): SteadyState, FailedOver, DRedSteadyState, FailedBack
// Transition states (4): FailingOver, Reprotecting, FailingBack, ReprotectingBack
const (
	PhaseSteadyState      = "SteadyState"
	PhaseFailingOver      = "FailingOver"
	PhaseFailedOver       = "FailedOver"
	PhaseReprotecting     = "Reprotecting"
	PhaseDRedSteadyState  = "DRedSteadyState"
	PhaseFailingBack      = "FailingBack"
	PhaseFailedBack       = "FailedBack"
	PhaseReprotectingBack = "ReprotectingBack"
)

// ConsistencyLevel determines how VM disks are grouped into VolumeGroups for
// atomic storage operations.
type ConsistencyLevel string

const (
	// ConsistencyLevelNamespace groups all VM disks in a namespace into a
	// single VolumeGroup for crash-consistent snapshots.
	ConsistencyLevelNamespace ConsistencyLevel = "namespace"
	// ConsistencyLevelVM treats each VM's disks as an independent VolumeGroup.
	ConsistencyLevelVM ConsistencyLevel = "vm"
)

// ConsistencyAnnotation is the namespace annotation key that controls
// consistency-level grouping. When set to "namespace", all VMs in that
// namespace share a single VolumeGroup.
const ConsistencyAnnotation = "soteria.io/consistency-level"

// DRPlanLabel is the label key that VMs use to declare membership in a DRPlan.
// Because a Kubernetes label key can only have one value per resource, this
// structurally enforces one-plan-per-VM exclusivity without runtime checks.
const DRPlanLabel = "soteria.io/drplan"

// DRPlan defines a disaster recovery plan for a set of VMs selected by labels.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DRPlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DRPlanSpec   `json:"spec"`
	Status            DRPlanStatus `json:"status,omitempty"`
}

type DRPlanSpec struct {
	// WaveLabel is the label key used to assign VMs to execution waves.
	WaveLabel string `json:"waveLabel"`
	// MaxConcurrentFailovers limits concurrent VM failovers per wave chunk.
	// +kubebuilder:validation:Minimum=1
	MaxConcurrentFailovers int `json:"maxConcurrentFailovers"`
	// PrimarySite is the cluster name that originally owns the active workloads.
	// Immutable after creation.
	PrimarySite string `json:"primarySite"`
	// SecondarySite is the cluster name that serves as the DR target.
	// Immutable after creation. Must differ from PrimarySite.
	SecondarySite string `json:"secondarySite"`
	// VMReadyTimeout is the maximum duration to wait for all VMs in a wave to
	// reach Running state after StartVM before declaring a timeout. Per-wave,
	// starts when all StartVM operations in the wave complete. Default: 5m.
	// +optional
	// +kubebuilder:default="5m"
	VMReadyTimeout *metav1.Duration `json:"vmReadyTimeout,omitempty"`
}

type DRPlanStatus struct {
	// Phase represents the current DR lifecycle rest state. Only rest-state
	// values are persisted; transient phases are derived at runtime via
	// engine.EffectivePhase(Phase, ActiveExecution mode).
	// Valid values: SteadyState, FailedOver, DRedSteadyState, FailedBack
	// +kubebuilder:validation:Enum=SteadyState;FailedOver;DRedSteadyState;FailedBack
	Phase string `json:"phase,omitempty"`
	// ActiveExecution is the name of the in-progress DRExecution, or empty
	// when no execution is running. Set by the reconciler on execution start
	// and cleared on completion or failure. Acts as an explicit concurrency
	// guard — the admission webhook rejects new executions while non-empty.
	ActiveExecution string `json:"activeExecution,omitempty"`
	// ActiveExecutionMode is the mode of the active execution, stored
	// alongside ActiveExecution so the table convertor can compute the
	// effective phase without an extra DRExecution GET.
	// +kubebuilder:validation:Enum=planned_migration;disaster;reprotect
	ActiveExecutionMode ExecutionMode `json:"activeExecutionMode,omitempty"`
	// ActiveSite tracks which cluster currently owns the active workloads.
	// Set to PrimarySite on creation; flipped to SecondarySite on failover
	// completion and back to PrimarySite on failback completion.
	ActiveSite string `json:"activeSite,omitempty"`
	// Conditions represent the latest observations of the DRPlan's state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the most recent generation observed.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Waves contains the discovered VMs grouped by wave label value.
	Waves []WaveInfo `json:"waves,omitempty"`
	// DiscoveredVMCount is the total number of VMs discovered for this plan.
	DiscoveredVMCount int `json:"discoveredVMCount,omitempty"`
	// Preflight contains the pre-flight plan composition report, populated on
	// every reconcile to give platform engineers visibility into plan structure
	// before execution.
	Preflight *PreflightReport `json:"preflight,omitempty"`
	// ReplicationHealth reports per-volume-group replication status,
	// populated by polling storage drivers on each reconcile cycle.
	// +listType=atomic
	ReplicationHealth []VolumeGroupHealth `json:"replicationHealth,omitempty"`
}

// PreflightReport is the pre-flight composition summary for a DRPlan. It
// assembles discovery, consistency, chunking, and storage backend data into
// a single user-facing structure that shows exactly how the plan would execute.
type PreflightReport struct {
	// PrimarySite is the declared primary cluster for this plan.
	PrimarySite string `json:"primarySite,omitempty"`
	// SecondarySite is the declared secondary (DR target) cluster.
	SecondarySite string `json:"secondarySite,omitempty"`
	// ActiveSite is the cluster currently owning the active workloads.
	ActiveSite string `json:"activeSite,omitempty"`
	// ActiveExecution is the name of the in-progress DRExecution, or empty
	// when idle. When non-empty, a warning is added to the report.
	ActiveExecution string `json:"activeExecution,omitempty"`
	// Waves contains per-wave composition summaries.
	// +listType=atomic
	Waves []PreflightWave `json:"waves,omitempty"`
	// TotalVMs is the total number of VMs in the plan.
	TotalVMs int `json:"totalVMs"`
	// Warnings contains non-blocking validation issues (e.g., unknown storage backend).
	// +listType=atomic
	Warnings []string `json:"warnings,omitempty"`
	// GeneratedAt is when this report was last computed.
	GeneratedAt *metav1.Time `json:"generatedAt,omitempty"`
}

// PreflightWave summarises a single execution wave in the pre-flight report.
type PreflightWave struct {
	// WaveKey is the wave label value.
	WaveKey string `json:"waveKey"`
	// VMCount is the total VMs in this wave.
	VMCount int `json:"vmCount"`
	// VMs contains per-VM composition details.
	// +listType=atomic
	VMs []PreflightVM `json:"vms,omitempty"`
	// Chunks contains the DRGroup chunking preview for this wave.
	// +listType=atomic
	Chunks []PreflightChunk `json:"chunks,omitempty"`
}

// PreflightVM describes a single VM's composition attributes in the pre-flight report.
type PreflightVM struct {
	// Name is the VM resource name.
	Name string `json:"name"`
	// Namespace is the VM's namespace.
	Namespace string `json:"namespace"`
	// StorageBackend is the driver name resolved from PVC storage class (or "unknown").
	StorageBackend string `json:"storageBackend"`
	// ConsistencyLevel is "namespace" or "vm".
	ConsistencyLevel string `json:"consistencyLevel"`
	// VolumeGroupName is the volume group this VM belongs to.
	VolumeGroupName string `json:"volumeGroupName"`
}

// PreflightChunk describes a DRGroup chunk in the pre-flight chunking preview.
type PreflightChunk struct {
	// Name is the DRGroup chunk name (e.g., "wave-1-group-0").
	Name string `json:"name"`
	// VMCount is the number of VMs in this chunk.
	VMCount int `json:"vmCount"`
	// VMNames lists the VM names in this chunk.
	// +listType=atomic
	VMNames []string `json:"vmNames,omitempty"`
	// VolumeGroups lists the volume group names in this chunk.
	// +listType=atomic
	VolumeGroups []string `json:"volumeGroups,omitempty"`
}

// VolumeGroupHealthStatus classifies replication health for a volume group.
// These values are higher-level than the driver's ReplicationHealth enum:
// HealthStatusError maps from a Go error returned by GetReplicationStatus,
// not from a driver health constant.
type VolumeGroupHealthStatus string

const (
	HealthStatusHealthy        VolumeGroupHealthStatus = "Healthy"
	HealthStatusDegraded       VolumeGroupHealthStatus = "Degraded"
	HealthStatusSyncing        VolumeGroupHealthStatus = "Syncing"
	HealthStatusNotReplicating VolumeGroupHealthStatus = "NotReplicating"
	HealthStatusError          VolumeGroupHealthStatus = "Error"
	HealthStatusUnknown        VolumeGroupHealthStatus = "Unknown"
)

// VolumeGroupHealth reports the replication health for a single volume group
// within a DRPlan. Populated by the DRPlan controller on each reconcile cycle
// from storage driver polling.
type VolumeGroupHealth struct {
	// Name is the volume group identifier (e.g. "ns-erp-database").
	Name string `json:"name"`
	// Namespace is the Kubernetes namespace for VMs in this group.
	Namespace string `json:"namespace"`
	// Health is the replication health status.
	// +kubebuilder:validation:Enum=Healthy;Degraded;Syncing;NotReplicating;Error;Unknown
	Health VolumeGroupHealthStatus `json:"health"`
	// LastSyncTime is when data was last successfully synchronized.
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`
	// LastChecked is when this health status was last polled.
	LastChecked metav1.Time `json:"lastChecked"`
	// Message contains an optional error or informational message.
	Message string `json:"message,omitempty"`
}

// DiscoveredVM identifies a VM discovered by a DRPlan's label selector.
type DiscoveredVM struct {
	// Name is the VM resource name.
	Name string `json:"name"`
	// Namespace is the VM's namespace.
	Namespace string `json:"namespace"`
}

// VolumeGroupInfo describes a group of VM disks that must be snapshotted
// atomically. Namespace-level groups ensure crash-consistent snapshots across
// all VMs sharing a namespace; VM-level groups (the default) scope consistency
// to a single VM's disks.
type VolumeGroupInfo struct {
	// Name is the group identifier (e.g. "ns-erp-database" or "vm-default-web01").
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Namespace is the Kubernetes namespace for VMs in this group.
	Namespace string `json:"namespace"`
	// ConsistencyLevel indicates whether this is a namespace- or VM-level group.
	// +kubebuilder:validation:Enum=namespace;vm
	ConsistencyLevel ConsistencyLevel `json:"consistencyLevel"`
	// VMNames lists the VMs belonging to this volume group.
	// +kubebuilder:validation:MinItems=1
	VMNames []string `json:"vmNames"`
}

// WaveInfo groups discovered VMs into a single execution wave.
// Invariant: a WaveInfo is only created when at least one VM belongs to the wave.
type WaveInfo struct {
	// WaveKey is the value of the wave label that groups these VMs.
	WaveKey string `json:"waveKey"`
	// VMs lists the discovered VMs in this wave.
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	VMs []DiscoveredVM `json:"vms"`
	// Groups contains the volume groups formed from VMs in this wave.
	// Populated after consistency resolution succeeds.
	Groups []VolumeGroupInfo `json:"groups,omitempty"`
}

// DRPlanList contains a list of DRPlans.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DRPlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DRPlan `json:"items"`
}

// ExecutionMode defines how a DRPlan is executed.
type ExecutionMode string

const (
	ExecutionModePlannedMigration ExecutionMode = "planned_migration"
	ExecutionModeDisaster         ExecutionMode = "disaster"
	ExecutionModeReprotect        ExecutionMode = "reprotect"
)

// ExecutionResult is the overall outcome of a DRExecution.
type ExecutionResult string

const (
	ExecutionResultSucceeded          ExecutionResult = "Succeeded"
	ExecutionResultPartiallySucceeded ExecutionResult = "PartiallySucceeded"
	ExecutionResultFailed             ExecutionResult = "Failed"
)

// DRGroupResult is the outcome of a single DRGroup within a wave.
type DRGroupResult string

const (
	DRGroupResultPending           DRGroupResult = "Pending"
	DRGroupResultInProgress        DRGroupResult = "InProgress"
	DRGroupResultCompleted         DRGroupResult = "Completed"
	DRGroupResultFailed            DRGroupResult = "Failed"
	DRGroupResultWaitingForVMReady DRGroupResult = "WaitingForVMReady"
)

// DRExecution records an immutable execution of a DRPlan.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DRExecution struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DRExecutionSpec   `json:"spec"`
	Status            DRExecutionStatus `json:"status,omitempty"`
}

type DRExecutionSpec struct {
	// PlanName references the DRPlan being executed.
	PlanName string `json:"planName"`
	// Mode specifies the execution type — chosen at runtime, not on the plan.
	// +kubebuilder:validation:Enum=planned_migration;disaster;reprotect
	Mode ExecutionMode `json:"mode"`
}

type DRExecutionStatus struct {
	// Result is the overall execution outcome.
	// +kubebuilder:validation:Enum=Succeeded;PartiallySucceeded;Failed
	Result ExecutionResult `json:"result,omitempty"`
	// Waves contains per-wave execution status.
	Waves []WaveStatus `json:"waves,omitempty"`
	// StartTime is when execution began.
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// CompletionTime is when execution finished.
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	// Conditions represent the latest observations.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type WaveStatus struct {
	// WaveIndex is the 0-based wave ordinal.
	WaveIndex int `json:"waveIndex"`
	// Groups contains per-DRGroup status within this wave.
	Groups []DRGroupExecutionStatus `json:"groups,omitempty"`
	// StartTime is when this wave began.
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// CompletionTime is when this wave finished.
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	// VMReadyStartTime is when the wave entered the WaitingForVMReady state
	// (all handler operations complete, now waiting for VMs to reach Running).
	// Used as the base for VMReadyTimeout calculation.
	VMReadyStartTime *metav1.Time `json:"vmReadyStartTime,omitempty"`
}

type DRGroupExecutionStatus struct {
	// Name identifies this DRGroup within the wave.
	Name string `json:"name"`
	// Result is the outcome of this DRGroup.
	// +kubebuilder:validation:Enum=Pending;InProgress;Completed;Failed;WaitingForVMReady
	Result DRGroupResult `json:"result,omitempty"`
	// VMNames lists VMs in this DRGroup.
	VMNames []string `json:"vmNames,omitempty"`
	// Error contains error details if the group failed.
	Error string `json:"error,omitempty"`
	// Steps records per-step execution details within this DRGroup.
	// +listType=atomic
	Steps []StepStatus `json:"steps,omitempty"`
	// RetryCount tracks how many times this group has been retried.
	// Incremented on each retry attempt for audit trail purposes.
	RetryCount int `json:"retryCount,omitempty"`
	// StartTime is when this group began processing.
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// CompletionTime is when this group finished.
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
}

// DRExecutionList contains a list of DRExecutions.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DRExecutionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DRExecution `json:"items"`
}

// DRGroupStatus tracks the real-time state of a DRGroup during execution.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DRGroupStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DRGroupStatusSpec  `json:"spec"`
	Status            DRGroupStatusState `json:"status,omitempty"`
}

type DRGroupStatusSpec struct {
	// ExecutionName references the parent DRExecution.
	ExecutionName string `json:"executionName"`
	// WaveIndex is the wave this group belongs to.
	WaveIndex int `json:"waveIndex"`
	// GroupName is the name of this DRGroup within the wave.
	GroupName string `json:"groupName"`
	// VMNames lists VMs in this group.
	VMNames []string `json:"vmNames,omitempty"`
}

type DRGroupStatusState struct {
	// Phase is the current processing state.
	// +kubebuilder:validation:Enum=Pending;InProgress;Completed;Failed;WaitingForVMReady
	Phase DRGroupResult `json:"phase,omitempty"`
	// Conditions represent the latest observations.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Steps records per-step execution details.
	Steps []StepStatus `json:"steps,omitempty"`
	// LastTransitionTime is when the phase last changed.
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

type StepStatus struct {
	// Name describes this step (e.g., "PromoteVolume", "StartVM").
	Name string `json:"name"`
	// Status is the step outcome.
	Status string `json:"status,omitempty"`
	// Message provides human-readable detail.
	Message string `json:"message,omitempty"`
	// Timestamp is when this step completed.
	Timestamp *metav1.Time `json:"timestamp,omitempty"`
}

// DRGroupStatusList contains a list of DRGroupStatuses.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DRGroupStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DRGroupStatus `json:"items"`
}
