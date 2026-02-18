package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type AgentSelectorSpec struct {
	Names       []string          `json:"names,omitempty"`
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

type AgentToolPolicySpec struct {
	AgentSelector     AgentSelectorSpec      `json:"agentSelector,omitempty"`
	AllowedSkillRefs  []LocalObjectReference `json:"allowedSkillRefs,omitempty"`
	AllowedProtocols  []string               `json:"allowedProtocols,omitempty"`
	ApprovalMode      string                 `json:"approvalMode,omitempty"`
	MaxCallsPerMinute int32                  `json:"maxCallsPerMinute,omitempty"`
}

type AgentToolPolicyStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Enforced           bool               `json:"enforced,omitempty"`
	Violations         int32              `json:"violations,omitempty"`
	LastViolationTime  *metav1.Time       `json:"lastViolationTime,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=atp
type AgentToolPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentToolPolicySpec   `json:"spec,omitempty"`
	Status AgentToolPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type AgentToolPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentToolPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentToolPolicy{}, &AgentToolPolicyList{})
}
