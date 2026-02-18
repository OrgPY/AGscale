package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type IntegrationTLS struct {
	InsecureSkipVerify bool                  `json:"insecureSkipVerify,omitempty"`
	CASecretRef        *LocalObjectReference `json:"caSecretRef,omitempty"`
}

type IntegrationAuth struct {
	SecretRef *SecretKeyReference `json:"secretRef,omitempty"`
}

type IntegrationRateLimit struct {
	RequestsPerMinute int32 `json:"requestsPerMinute,omitempty"`
}

type IntegrationConnection struct {
	URL            string            `json:"url,omitempty"`
	TimeoutSeconds int32             `json:"timeoutSeconds,omitempty"`
	Auth           IntegrationAuth   `json:"auth,omitempty"`
	TLS            IntegrationTLS    `json:"tls,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
}

type IntegrationEndpointSpec struct {
	Type         string                `json:"type,omitempty"`
	Connection   IntegrationConnection `json:"connection,omitempty"`
	Capabilities []string              `json:"capabilities,omitempty"`
	RateLimit    IntegrationRateLimit  `json:"rateLimit,omitempty"`
}

type IntegrationEndpointStatus struct {
	ObservedGeneration  int64              `json:"observedGeneration,omitempty"`
	Ready               bool               `json:"ready,omitempty"`
	LastHealthCheckTime *metav1.Time       `json:"lastHealthCheckTime,omitempty"`
	LastError           string             `json:"lastError,omitempty"`
	Conditions          []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=iep
type IntegrationEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IntegrationEndpointSpec   `json:"spec,omitempty"`
	Status IntegrationEndpointStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type IntegrationEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IntegrationEndpoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IntegrationEndpoint{}, &IntegrationEndpointList{})
}
