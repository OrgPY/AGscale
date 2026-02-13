package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AgentRuntimeSpec struct {
	Image           string                      `json:"image"`
	ImagePullPolicy corev1.PullPolicy           `json:"imagePullPolicy,omitempty"`
	Command         []string                    `json:"command,omitempty"`
	Args            []string                    `json:"args,omitempty"`
	Env             []corev1.EnvVar             `json:"env,omitempty"`
	Ports           []corev1.ContainerPort      `json:"ports,omitempty"`
	Resources       corev1.ResourceRequirements `json:"resources,omitempty"`
	LivenessProbe   *corev1.Probe               `json:"livenessProbe,omitempty"`
	ReadinessProbe  *corev1.Probe               `json:"readinessProbe,omitempty"`
	SecurityContext *corev1.SecurityContext     `json:"securityContext,omitempty"`
	PodAnnotations  map[string]string           `json:"podAnnotations,omitempty"`
	PodLabels       map[string]string           `json:"podLabels,omitempty"`
}

type AgentRuntimeStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=art
type AgentRuntime struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentRuntimeSpec   `json:"spec,omitempty"`
	Status AgentRuntimeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type AgentRuntimeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentRuntime `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentRuntime{}, &AgentRuntimeList{})
}
