package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type EventSourceDelivery struct {
	Mode string `json:"mode,omitempty"`
}

type EventSourceSpec struct {
	IntegrationRef    LocalObjectReference `json:"integrationRef,omitempty"`
	Topic             string               `json:"topic,omitempty"`
	Filter            string               `json:"filter,omitempty"`
	Delivery          EventSourceDelivery  `json:"delivery,omitempty"`
	TargetWorkflowRef LocalObjectReference `json:"targetWorkflowRef,omitempty"`
}

type EventSourceStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Phase              string             `json:"phase,omitempty"`
	Lag                int64              `json:"lag,omitempty"`
	LastEventTime      *metav1.Time       `json:"lastEventTime,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=esrc
type EventSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EventSourceSpec   `json:"spec,omitempty"`
	Status EventSourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type EventSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EventSource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EventSource{}, &EventSourceList{})
}
