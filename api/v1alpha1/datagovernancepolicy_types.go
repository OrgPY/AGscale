package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type DataGovernancePolicySpec struct {
	PIIRedaction        []string `json:"piiRedaction,omitempty"`
	AllowedDestinations []string `json:"allowedDestinations,omitempty"`
	ForbiddenFields     []string `json:"forbiddenFields,omitempty"`
	RetentionDays       int32    `json:"retentionDays,omitempty"`
}

type DataGovernancePolicyStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=dgp
type DataGovernancePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataGovernancePolicySpec   `json:"spec,omitempty"`
	Status DataGovernancePolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type DataGovernancePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataGovernancePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DataGovernancePolicy{}, &DataGovernancePolicyList{})
}
