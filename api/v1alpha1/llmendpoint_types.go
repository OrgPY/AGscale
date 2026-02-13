package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type LLMAuthSpec struct {
	SecretRef SecretKeyReference `json:"secretRef,omitempty"`
}

type LLMRateLimitSpec struct {
	RequestsPerMinute int32 `json:"requestsPerMinute,omitempty"`
	TokensPerMinute   int32 `json:"tokensPerMinute,omitempty"`
}

type LLMEndpointSpec struct {
	Provider       string            `json:"provider"`
	BaseURL        string            `json:"baseURL,omitempty"`
	DefaultModel   string            `json:"defaultModel,omitempty"`
	Auth           LLMAuthSpec       `json:"auth,omitempty"`
	RateLimit      LLMRateLimitSpec  `json:"rateLimit,omitempty"`
	TimeoutSeconds int32             `json:"timeoutSeconds,omitempty"`
	ExtraHeaders   map[string]string `json:"extraHeaders,omitempty"`
}

type LLMEndpointStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=llm
type LLMEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LLMEndpointSpec   `json:"spec,omitempty"`
	Status LLMEndpointStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type LLMEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LLMEndpoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LLMEndpoint{}, &LLMEndpointList{})
}
