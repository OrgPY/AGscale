package controller

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	agscalev1alpha1 "agscale/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type IntegrationEndpointReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Recorder   record.EventRecorder
	HTTPClient *http.Client
}

func (r *IntegrationEndpointReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	started := time.Now()
	result := "success"
	defer func() { observeReconcile("IntegrationEndpoint", result, started) }()

	endpoint := &agscalev1alpha1.IntegrationEndpoint{}
	if err := r.Get(ctx, req.NamespacedName, endpoint); err != nil {
		result = "not_found"
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	now := metav1.Now()
	endpoint.Status.ObservedGeneration = endpoint.Generation
	endpoint.Status.LastHealthCheckTime = &now

	probeURL := integrationProbeURL(endpoint)
	if probeURL == "" {
		endpoint.Status.Ready = false
		endpoint.Status.LastError = "connection.url or a2a.baseURL is required"
		setConditionWorkflow(&endpoint.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "MissingURL", Message: endpoint.Status.LastError, LastTransitionTime: now, ObservedGeneration: endpoint.Generation})
		if err := r.Status().Update(ctx, endpoint); err != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("IntegrationEndpoint", "StatusUpdate").Inc()
			return ctrl.Result{}, err
		}
		integrationHealthChecksTotal.WithLabelValues(endpoint.Name, "error").Inc()
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	timeout := time.Duration(endpoint.Spec.Connection.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	transport := &http.Transport{}
	tlsConfig := integrationProbeTLS(endpoint)
	if tlsConfig.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	hc := r.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: timeout, Transport: transport}
	}
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		endpoint.Status.Ready = false
		endpoint.Status.LastError = err.Error()
		setConditionWorkflow(&endpoint.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "InvalidURL", Message: err.Error(), LastTransitionTime: now, ObservedGeneration: endpoint.Generation})
		_ = r.Status().Update(ctx, endpoint)
		integrationHealthChecksTotal.WithLabelValues(endpoint.Name, "error").Inc()
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}
	for k, v := range integrationProbeHeaders(endpoint) {
		reqHTTP.Header.Set(k, v)
	}
	if secretRef := integrationProbeSecretRef(endpoint); secretRef != nil && secretRef.Name != "" {
		secret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: endpoint.Namespace, Name: secretRef.Name}, secret); err == nil {
			key := secretRef.Key
			if key == "" {
				key = "token"
			}
			if token, ok := secret.Data[key]; ok {
				reqHTTP.Header.Set("Authorization", "Bearer "+string(token))
			}
		}
	}

	resp, err := hc.Do(reqHTTP)
	if err != nil {
		endpoint.Status.Ready = false
		endpoint.Status.LastError = err.Error()
		setConditionWorkflow(&endpoint.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "HealthCheckFailed", Message: err.Error(), LastTransitionTime: now, ObservedGeneration: endpoint.Generation})
		if err := r.Status().Update(ctx, endpoint); err != nil {
			result = "error"
			reconcileErrorsTotal.WithLabelValues("IntegrationEndpoint", "StatusUpdate").Inc()
			return ctrl.Result{}, err
		}
		integrationHealthChecksTotal.WithLabelValues(endpoint.Name, "error").Inc()
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		endpoint.Status.Ready = false
		endpoint.Status.LastError = fmt.Sprintf("health check returned %d", resp.StatusCode)
		setConditionWorkflow(&endpoint.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Unhealthy", Message: endpoint.Status.LastError, LastTransitionTime: now, ObservedGeneration: endpoint.Generation})
		integrationHealthChecksTotal.WithLabelValues(endpoint.Name, "error").Inc()
	} else {
		endpoint.Status.Ready = true
		endpoint.Status.LastError = ""
		setConditionWorkflow(&endpoint.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Healthy", Message: "integration endpoint healthy", LastTransitionTime: now, ObservedGeneration: endpoint.Generation})
		integrationHealthChecksTotal.WithLabelValues(endpoint.Name, "success").Inc()
	}
	if err := r.Status().Update(ctx, endpoint); err != nil {
		result = "error"
		reconcileErrorsTotal.WithLabelValues("IntegrationEndpoint", "StatusUpdate").Inc()
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

func (r *IntegrationEndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agscalev1alpha1.IntegrationEndpoint{}).
		Complete(r)
}

func integrationProbeURL(endpoint *agscalev1alpha1.IntegrationEndpoint) string {
	if endpoint == nil {
		return ""
	}
	if url := strings.TrimSpace(endpoint.Spec.Connection.URL); url != "" {
		return url
	}
	return strings.TrimSpace(endpoint.Spec.A2A.BaseURL)
}

func integrationProbeTLS(endpoint *agscalev1alpha1.IntegrationEndpoint) agscalev1alpha1.IntegrationTLS {
	if endpoint == nil {
		return agscalev1alpha1.IntegrationTLS{}
	}
	if strings.TrimSpace(endpoint.Spec.Connection.URL) != "" {
		return endpoint.Spec.Connection.TLS
	}
	return endpoint.Spec.A2A.TLS
}

func integrationProbeHeaders(endpoint *agscalev1alpha1.IntegrationEndpoint) map[string]string {
	headers := map[string]string{}
	if endpoint == nil {
		return headers
	}
	for k, v := range endpoint.Spec.Connection.Headers {
		headers[k] = v
	}
	if strings.TrimSpace(endpoint.Spec.Connection.URL) == "" {
		for k, v := range endpoint.Spec.A2A.Headers {
			headers[k] = v
		}
	}
	return headers
}

func integrationProbeSecretRef(endpoint *agscalev1alpha1.IntegrationEndpoint) *agscalev1alpha1.SecretKeyReference {
	if endpoint == nil {
		return nil
	}
	if strings.TrimSpace(endpoint.Spec.Connection.URL) != "" {
		if endpoint.Spec.Connection.Auth.SecretRef != nil {
			return endpoint.Spec.Connection.Auth.SecretRef
		}
		return endpoint.Spec.A2A.Auth.SecretRef
	}
	if endpoint.Spec.A2A.Auth.SecretRef != nil {
		return endpoint.Spec.A2A.Auth.SecretRef
	}
	return endpoint.Spec.Connection.Auth.SecretRef
}
