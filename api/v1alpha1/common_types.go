package v1alpha1

type LocalObjectReference struct {
	Name string `json:"name"`
}

type SecretKeyReference struct {
	Name string `json:"name"`
	Key  string `json:"key,omitempty"`
}
