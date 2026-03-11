package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type WorkflowMode string

type WorkflowStepType string
type WorkflowProtocol string

const (
	WorkflowModeRequestResponse WorkflowMode = "RequestResponse"
	WorkflowModeScheduled       WorkflowMode = "Scheduled"
	WorkflowModeEventDriven     WorkflowMode = "EventDriven"

	WorkflowStepPrompt    WorkflowStepType = "Prompt"
	WorkflowStepRetrieve  WorkflowStepType = "Retrieve"
	WorkflowStepToolCall  WorkflowStepType = "ToolCall"
	WorkflowStepTransform WorkflowStepType = "Transform"
	WorkflowStepRoute     WorkflowStepType = "Route"

	WorkflowProtocolHTTP WorkflowProtocol = "http"
	WorkflowProtocolMCP  WorkflowProtocol = "mcp"
	WorkflowProtocolA2A  WorkflowProtocol = "a2a"
)

type WorkflowStepRetryPolicy struct {
	MaxRetries     int32 `json:"maxRetries,omitempty"`
	BackoffSeconds int32 `json:"backoffSeconds,omitempty"`
}

type WorkflowStepSpec struct {
	Name           string                  `json:"name,omitempty"`
	AgentRef       *LocalObjectReference   `json:"agentRef,omitempty"`
	IntegrationRef *LocalObjectReference   `json:"integrationRef,omitempty"`
	DependsOn      []string                `json:"dependsOn,omitempty"`
	Protocol       WorkflowProtocol        `json:"protocol,omitempty"`
	EndpointPath   string                  `json:"endpointPath,omitempty"`
	Type           WorkflowStepType        `json:"type,omitempty"`
	TimeoutSeconds int32                   `json:"timeoutSeconds,omitempty"`
	RetryPolicy    WorkflowStepRetryPolicy `json:"retryPolicy,omitempty"`
}

type WorkflowRoutingSpec struct {
	OnSuccess string `json:"onSuccess,omitempty"`
	OnFailure string `json:"onFailure,omitempty"`
}

type WorkflowScheduleSpec struct {
	Cron string `json:"cron,omitempty"`
}

type WorkflowPolicyRefs struct {
	ReliabilityPolicyRef    *LocalObjectReference `json:"reliabilityPolicyRef,omitempty"`
	BudgetPolicyRef         *LocalObjectReference `json:"budgetPolicyRef,omitempty"`
	ToolPolicyRef           *LocalObjectReference `json:"toolPolicyRef,omitempty"`
	DataGovernancePolicyRef *LocalObjectReference `json:"dataGovernancePolicyRef,omitempty"`
}

type AgentWorkflowSpec struct {
	EntryAgentRef  LocalObjectReference  `json:"entryAgentRef,omitempty"`
	Mode           WorkflowMode          `json:"mode,omitempty"`
	Steps          []WorkflowStepSpec    `json:"steps,omitempty"`
	Routing        WorkflowRoutingSpec   `json:"routing,omitempty"`
	Schedule       WorkflowScheduleSpec  `json:"schedule,omitempty"`
	EventSourceRef *LocalObjectReference `json:"eventSourceRef,omitempty"`
	PolicyRefs     WorkflowPolicyRefs    `json:"policyRefs,omitempty"`
}

type AgentWorkflowStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Phase              string             `json:"phase,omitempty"`
	LastRunTime        *metav1.Time       `json:"lastRunTime,omitempty"`
	LastRunID          string             `json:"lastRunID,omitempty"`
	RunCount           int64              `json:"runCount,omitempty"`
	FailureCount       int64              `json:"failureCount,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=awf
type AgentWorkflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentWorkflowSpec   `json:"spec,omitempty"`
	Status AgentWorkflowStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type AgentWorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentWorkflow `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentWorkflow{}, &AgentWorkflowList{})
}
