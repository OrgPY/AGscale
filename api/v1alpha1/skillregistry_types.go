package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type SkillRegistryTLS struct {
	InsecureSkipVerify bool                  `json:"insecureSkipVerify,omitempty"`
	CASecretRef        *LocalObjectReference `json:"caSecretRef,omitempty"`
}

type SkillRegistrySync struct {
	Enabled         bool  `json:"enabled,omitempty"`
	IntervalSeconds int32 `json:"intervalSeconds,omitempty"`
}

type SkillRegistryRateLimit struct {
	QPS   int32 `json:"qps,omitempty"`
	Burst int32 `json:"burst,omitempty"`
}

type SkillRegistryFilters struct {
	IncludeTags []string `json:"includeTags,omitempty"`
	ExcludeTags []string `json:"excludeTags,omitempty"`
	NameRegex   string   `json:"nameRegex,omitempty"`
}

type SkillRegistryOpenAPI struct {
	ListPath        string            `json:"listPath,omitempty"`
	GetPathTemplate string            `json:"getPathTemplate,omitempty"`
	Mapping         map[string]string `json:"mapping,omitempty"`
}

type SkillRegistryMCP struct {
	Transport         string            `json:"transport,omitempty"`
	ServerID          string            `json:"serverId,omitempty"`
	ToolsPath         string            `json:"toolsPath,omitempty"`
	ResourcesPath     string            `json:"resourcesPath,omitempty"`
	TemplatesPath     string            `json:"templatesPath,omitempty"`
	CapabilityMapping map[string]string `json:"capabilityMapping,omitempty"`
}

type SkillRegistrySpec struct {
	Protocol  string                 `json:"protocol"`
	BaseURL   string                 `json:"baseURL"`
	Auth      *SecretKeyReference    `json:"auth,omitempty"`
	TLS       SkillRegistryTLS       `json:"tls,omitempty"`
	Sync      SkillRegistrySync      `json:"sync,omitempty"`
	RateLimit SkillRegistryRateLimit `json:"rateLimit,omitempty"`
	Filters   SkillRegistryFilters   `json:"filters,omitempty"`
	OpenAPI   SkillRegistryOpenAPI   `json:"openapi,omitempty"`
	MCP       SkillRegistryMCP       `json:"mcp,omitempty"`
}

type SkillRegistryStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	LastSyncTime       *metav1.Time       `json:"lastSyncTime,omitempty"`
	LastError          string             `json:"lastError,omitempty"`
	DiscoveredCount    int32              `json:"discoveredCount,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=sr
type SkillRegistry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SkillRegistrySpec   `json:"spec,omitempty"`
	Status SkillRegistryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type SkillRegistryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SkillRegistry `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SkillRegistry{}, &SkillRegistryList{})
}
