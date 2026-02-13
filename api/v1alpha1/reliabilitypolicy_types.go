package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type ReliabilitySLOSpec struct {
	AvailabilityTarget string  `json:"availabilityTarget,omitempty"`
	LatencyP95Ms       int32   `json:"latencyP95Ms,omitempty"`
	ErrorRateThreshold float64 `json:"errorRateThreshold,omitempty"`
}

type ReliabilityHealthSpec struct {
	StartupGraceSeconds  int32 `json:"startupGraceSeconds,omitempty"`
	FailureWindowSeconds int32 `json:"failureWindowSeconds,omitempty"`
	MinReadyPercent      int32 `json:"minReadyPercent,omitempty"`
}

type ReliabilityDependencyCheck struct {
	Enabled                    bool  `json:"enabled,omitempty"`
	TimeoutMs                  int32 `json:"timeoutMs,omitempty"`
	ConsecutiveFailuresForOpen int32 `json:"consecutiveFailuresForOpen,omitempty"`
}

type ReliabilityDependencyChecksSpec struct {
	LLM     ReliabilityDependencyCheck `json:"llm,omitempty"`
	Memory  ReliabilityDependencyCheck `json:"memory,omitempty"`
	Context ReliabilityDependencyCheck `json:"context,omitempty"`
}

type ReliabilityRemediationSpec struct {
	Actions           []string `json:"actions,omitempty"`
	MaxActionsPerHour int32    `json:"maxActionsPerHour,omitempty"`
	CooldownSeconds   int32    `json:"cooldownSeconds,omitempty"`
}

type ReliabilityRollbackSpec struct {
	Enabled            bool     `json:"enabled,omitempty"`
	MaxRevisionHistory int32    `json:"maxRevisionHistory,omitempty"`
	TriggerOnCondition []string `json:"triggerOnCondition,omitempty"`
}

type AgentReliabilityPolicySpec struct {
	SLO              ReliabilitySLOSpec              `json:"slo,omitempty"`
	Health           ReliabilityHealthSpec           `json:"health,omitempty"`
	DependencyChecks ReliabilityDependencyChecksSpec `json:"dependencyChecks,omitempty"`
	Remediation      ReliabilityRemediationSpec      `json:"remediation,omitempty"`
	Rollback         ReliabilityRollbackSpec         `json:"rollback,omitempty"`
}

type AgentReliabilityPolicyStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=arp
type AgentReliabilityPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentReliabilityPolicySpec   `json:"spec,omitempty"`
	Status AgentReliabilityPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type AgentReliabilityPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentReliabilityPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentReliabilityPolicy{}, &AgentReliabilityPolicyList{})
}
