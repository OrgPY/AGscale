package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type BudgetScope string

type BudgetWindowMode string

type BudgetPhase string

const (
	BudgetScopeAgent     BudgetScope = "Agent"
	BudgetScopeNamespace BudgetScope = "Namespace"

	BudgetWindowRolling24h BudgetWindowMode = "Rolling24h"

	BudgetPhaseWithinBudget BudgetPhase = "WithinBudget"
	BudgetPhaseExceeded     BudgetPhase = "Exceeded"
	BudgetPhaseUnknown      BudgetPhase = "Unknown"
)

type BudgetTargetRef struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type BudgetWindowSpec struct {
	Mode BudgetWindowMode `json:"mode,omitempty"`
}

type BudgetLimitSpec struct {
	MaxRequests24h         *int64   `json:"maxRequests24h,omitempty"`
	MaxTokens24h           *int64   `json:"maxTokens24h,omitempty"`
	MaxEstimatedCostUSD24h *float64 `json:"maxEstimatedCostUSD24h,omitempty"`
}

type BudgetMetricsSourceSpec struct {
	Enabled *bool `json:"enabled,omitempty"`
}

type BudgetProviderSourceSpec struct {
	Type string `json:"type,omitempty"`
}

type BudgetUsageSourcesSpec struct {
	Metrics   BudgetMetricsSourceSpec    `json:"metrics,omitempty"`
	Providers []BudgetProviderSourceSpec `json:"providers,omitempty"`
}

type BudgetEvaluationSpec struct {
	IntervalSeconds   int32 `json:"intervalSeconds,omitempty"`
	StaleAfterSeconds int32 `json:"staleAfterSeconds,omitempty"`
}

type BudgetAlertingSpec struct {
	EmitK8sEvents *bool             `json:"emitK8sEvents,omitempty"`
	SetConditions *bool             `json:"setConditions,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type AgentBudgetPolicySpec struct {
	Scope        BudgetScope            `json:"scope,omitempty"`
	TargetRef    BudgetTargetRef        `json:"targetRef,omitempty"`
	Window       BudgetWindowSpec       `json:"window,omitempty"`
	Limits       BudgetLimitSpec        `json:"limits,omitempty"`
	UsageSources BudgetUsageSourcesSpec `json:"usageSources,omitempty"`
	Evaluation   BudgetEvaluationSpec   `json:"evaluation,omitempty"`
	Alerting     BudgetAlertingSpec     `json:"alerting,omitempty"`
}

type BudgetUsageSnapshot struct {
	Requests24h      int64   `json:"requests24h,omitempty"`
	Tokens24h        int64   `json:"tokens24h,omitempty"`
	EstimatedCostUSD float64 `json:"estimatedCostUSD,omitempty"`
}

type AgentBudgetPolicyStatus struct {
	ObservedGeneration int64               `json:"observedGeneration,omitempty"`
	LastEvaluatedTime  *metav1.Time        `json:"lastEvaluatedTime,omitempty"`
	WindowStartTime    *metav1.Time        `json:"windowStartTime,omitempty"`
	EffectiveUsage     BudgetUsageSnapshot `json:"effectiveUsage,omitempty"`
	EffectiveLimits    BudgetLimitSpec     `json:"effectiveLimits,omitempty"`
	Phase              BudgetPhase         `json:"phase,omitempty"`
	Reason             string              `json:"reason,omitempty"`
	Conditions         []metav1.Condition  `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=abp
type AgentBudgetPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentBudgetPolicySpec   `json:"spec,omitempty"`
	Status AgentBudgetPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type AgentBudgetPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentBudgetPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentBudgetPolicy{}, &AgentBudgetPolicyList{})
}
