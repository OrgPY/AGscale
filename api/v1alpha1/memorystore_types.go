package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type MemoryConnectionSpec struct {
	SecretRef *SecretKeyReference `json:"secretRef,omitempty"`
	Endpoint  string              `json:"endpoint,omitempty"`
	Database  string              `json:"database,omitempty"`
}

type MemoryRetentionSpec struct {
	TTLSeconds       int64 `json:"ttlSeconds,omitempty"`
	MaxItemsPerAgent int64 `json:"maxItemsPerAgent,omitempty"`
}

type MemoryConsistencySpec struct {
	Durability        string `json:"durability,omitempty"`
	ReplicationFactor int32  `json:"replicationFactor,omitempty"`
}

type MemoryStoreSpec struct {
	Type        string                `json:"type"`
	Connection  MemoryConnectionSpec  `json:"connection,omitempty"`
	Retention   MemoryRetentionSpec   `json:"retention,omitempty"`
	Consistency MemoryConsistencySpec `json:"consistency,omitempty"`
}

type MemoryStoreStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mem
type MemoryStore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MemoryStoreSpec   `json:"spec,omitempty"`
	Status MemoryStoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type MemoryStoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MemoryStore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MemoryStore{}, &MemoryStoreList{})
}
