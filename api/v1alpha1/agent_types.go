package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AgentServiceSpec struct {
	Enabled    bool               `json:"enabled,omitempty"`
	Port       int32              `json:"port,omitempty"`
	TargetPort int32              `json:"targetPort,omitempty"`
	Type       corev1.ServiceType `json:"type,omitempty"`
}

type AgentAutoscalingSpec struct {
	MinReplicas                    int32 `json:"minReplicas,omitempty"`
	MaxReplicas                    int32 `json:"maxReplicas,omitempty"`
	TargetCPUUtilizationPercentage int32 `json:"targetCPUUtilizationPercentage,omitempty"`
}

type AgentObservabilityMetricsSpec struct {
	Path string `json:"path,omitempty"`
	Port int32  `json:"port,omitempty"`
}

type AgentObservabilityTracingSpec struct {
	Enabled      bool   `json:"enabled,omitempty"`
	OTLPEndpoint string `json:"otelEndpoint,omitempty"`
}

type AgentObservabilitySpec struct {
	Enabled      *bool                         `json:"enabled,omitempty"`
	Metrics      AgentObservabilityMetricsSpec `json:"metrics,omitempty"`
	Tracing      AgentObservabilityTracingSpec `json:"tracing,omitempty"`
	AlertProfile string                        `json:"alertProfile,omitempty"`
}

type AgentSpec struct {
	RuntimeRef            LocalObjectReference        `json:"runtimeRef"`
	ReliabilityPolicyRef  *LocalObjectReference       `json:"reliabilityPolicyRef,omitempty"`
	BudgetPolicyRef       *LocalObjectReference       `json:"budgetPolicyRef,omitempty"`
	SkillResolutionPolicy string                      `json:"skillResolutionPolicy,omitempty"`
	Replicas              *int32                      `json:"replicas,omitempty"`
	LLMEndpointRef        *LocalObjectReference       `json:"llmEndpointRef,omitempty"`
	MemoryRefs            []LocalObjectReference      `json:"memoryRefs,omitempty"`
	ContextRefs           []LocalObjectReference      `json:"contextRefs,omitempty"`
	SkillRefs             []LocalObjectReference      `json:"skillRefs,omitempty"`
	Image                 string                      `json:"image,omitempty"`
	ImagePullPolicy       corev1.PullPolicy           `json:"imagePullPolicy,omitempty"`
	Command               []string                    `json:"command,omitempty"`
	Args                  []string                    `json:"args,omitempty"`
	Env                   []corev1.EnvVar             `json:"env,omitempty"`
	Volumes               []corev1.Volume             `json:"volumes,omitempty"`
	VolumeMounts          []corev1.VolumeMount        `json:"volumeMounts,omitempty"`
	Resources             corev1.ResourceRequirements `json:"resources,omitempty"`
	Service               AgentServiceSpec            `json:"service,omitempty"`
	Autoscaling           *AgentAutoscalingSpec       `json:"autoscaling,omitempty"`
	Observability         AgentObservabilitySpec      `json:"observability,omitempty"`
	PodAnnotations        map[string]string           `json:"podAnnotations,omitempty"`
	PodLabels             map[string]string           `json:"podLabels,omitempty"`
}

type RemediationRecord struct {
	Action    string      `json:"action,omitempty"`
	Reason    string      `json:"reason,omitempty"`
	Timestamp metav1.Time `json:"timestamp,omitempty"`
}

type AgentStatus struct {
	ObservedGeneration              int64               `json:"observedGeneration,omitempty"`
	ReadyReplicas                   int32               `json:"readyReplicas,omitempty"`
	ServiceName                     string              `json:"serviceName,omitempty"`
	URL                             string              `json:"url,omitempty"`
	LastRemediationAction           string              `json:"lastRemediationAction,omitempty"`
	LastRemediationTime             *metav1.Time        `json:"lastRemediationTime,omitempty"`
	RecentRemediations              []RemediationRecord `json:"recentRemediations,omitempty"`
	ObservabilityLastScrapeReady    bool                `json:"observabilityLastScrapeReady,omitempty"`
	ObservabilityLastTelemetryError string              `json:"observabilityLastTelemetryError,omitempty"`
	BudgetPhase                     string              `json:"budgetPhase,omitempty"`
	BudgetReason                    string              `json:"budgetReason,omitempty"`
	BudgetLastEvaluatedTime         *metav1.Time        `json:"budgetLastEvaluatedTime,omitempty"`
	BudgetUsageSnapshot             BudgetUsageSnapshot `json:"budgetUsageSnapshot,omitempty"`
	Conditions                      []metav1.Condition  `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ag
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentSpec   `json:"spec,omitempty"`
	Status AgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Agent{}, &AgentList{})
}
