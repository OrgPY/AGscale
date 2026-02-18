package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type WorkflowRunStepResult struct {
	StepName   string `json:"stepName,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Status     string `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
}

type WorkflowRunPolicyDecision struct {
	Policy string `json:"policy,omitempty"`
	Result string `json:"result,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type AgentWorkflowRunSpec struct {
	WorkflowRef   LocalObjectReference  `json:"workflowRef,omitempty"`
	Trigger       string                `json:"trigger,omitempty"`
	InputRef      *LocalObjectReference `json:"inputRef,omitempty"`
	CorrelationID string                `json:"correlationID,omitempty"`
}

type AgentWorkflowRunStatus struct {
	ObservedGeneration int64                       `json:"observedGeneration,omitempty"`
	Phase              string                      `json:"phase,omitempty"`
	StartedAt          *metav1.Time                `json:"startedAt,omitempty"`
	CompletedAt        *metav1.Time                `json:"completedAt,omitempty"`
	StepResults        []WorkflowRunStepResult     `json:"stepResults,omitempty"`
	OutputRef          *LocalObjectReference       `json:"outputRef,omitempty"`
	PolicyDecisions    []WorkflowRunPolicyDecision `json:"policyDecisions,omitempty"`
	Conditions         []metav1.Condition          `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=awr
type AgentWorkflowRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentWorkflowRunSpec   `json:"spec,omitempty"`
	Status AgentWorkflowRunStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type AgentWorkflowRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentWorkflowRun `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentWorkflowRun{}, &AgentWorkflowRunList{})
}
