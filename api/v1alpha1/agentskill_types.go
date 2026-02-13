package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type AgentSkillSourceSpec struct {
	Protocol      string              `json:"protocol"`
	URL           string              `json:"url"`
	ExternalID    string              `json:"externalID,omitempty"`
	AuthSecretRef *SecretKeyReference `json:"authSecretRef,omitempty"`
}

type AgentSkillSyncPolicySpec struct {
	Mode            string `json:"mode,omitempty"`
	IntervalSeconds int32  `json:"intervalSeconds,omitempty"`
}

type AgentSkillSpec struct {
	Type          string                   `json:"type,omitempty"`
	Description   string                   `json:"description,omitempty"`
	Endpoint      string                   `json:"endpoint,omitempty"`
	AuthSecretRef *SecretKeyReference      `json:"authSecretRef,omitempty"`
	Config        map[string]string        `json:"config,omitempty"`
	Source        *AgentSkillSourceSpec    `json:"source,omitempty"`
	SyncPolicy    AgentSkillSyncPolicySpec `json:"syncPolicy,omitempty"`
}

type AgentSkillStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	SourceResolved     bool               `json:"sourceResolved,omitempty"`
	ResolvedVersion    string             `json:"resolvedVersion,omitempty"`
	LastResolvedAt     *metav1.Time       `json:"lastResolvedAt,omitempty"`
	LastError          string             `json:"lastError,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=skill
type AgentSkill struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentSkillSpec   `json:"spec,omitempty"`
	Status AgentSkillStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type AgentSkillList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentSkill `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentSkill{}, &AgentSkillList{})
}
