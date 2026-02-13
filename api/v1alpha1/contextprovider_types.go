package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type ContextAuthSpec struct {
	SecretRef *SecretKeyReference `json:"secretRef,omitempty"`
}

type ContextProviderSpec struct {
	Type                   string            `json:"type"`
	RefreshIntervalSeconds int64             `json:"refreshIntervalSeconds,omitempty"`
	Auth                   ContextAuthSpec   `json:"auth,omitempty"`
	Config                 map[string]string `json:"config,omitempty"`
}

type ContextProviderStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	LastSyncTime       *metav1.Time       `json:"lastSyncTime,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ctx
type ContextProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ContextProviderSpec   `json:"spec,omitempty"`
	Status ContextProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ContextProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ContextProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ContextProvider{}, &ContextProviderList{})
}
