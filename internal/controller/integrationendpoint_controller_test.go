package controller

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	agscalev1alpha1 "agscale/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestIntegrationEndpointReconcileHealthTransitions(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		expectOK bool
	}{
		{name: "healthy", status: http.StatusOK, expectOK: true},
		{name: "unhealthy", status: http.StatusServiceUnavailable, expectOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := agscalev1alpha1.AddToScheme(scheme); err != nil {
				t.Fatalf("add scheme: %v", err)
			}

			iep := &agscalev1alpha1.IntegrationEndpoint{
				ObjectMeta: metav1.ObjectMeta{Name: "iep-a", Namespace: "default"},
				Spec: agscalev1alpha1.IntegrationEndpointSpec{
					Type: "webhook",
					Connection: agscalev1alpha1.IntegrationConnection{
						URL:            "https://integration.example/health",
						TimeoutSeconds: 3,
					},
				},
			}

			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(iep).
				WithObjects(iep).
				Build()

			httpClient := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tc.status,
					Body:       io.NopCloser(strings.NewReader("ok")),
					Header:     make(http.Header),
				}, nil
			})}

			reconciler := &IntegrationEndpointReconciler{Client: c, Scheme: scheme, HTTPClient: httpClient}
			ctx := context.Background()
			if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: iep.Name, Namespace: iep.Namespace}}); err != nil {
				t.Fatalf("reconcile: %v", err)
			}

			updated := &agscalev1alpha1.IntegrationEndpoint{}
			if err := c.Get(ctx, types.NamespacedName{Name: iep.Name, Namespace: iep.Namespace}, updated); err != nil {
				t.Fatalf("get iep: %v", err)
			}
			if updated.Status.Ready != tc.expectOK {
				t.Fatalf("expected ready=%t, got %t", tc.expectOK, updated.Status.Ready)
			}
			if tc.expectOK && updated.Status.LastError != "" {
				t.Fatalf("expected empty lastError when healthy, got %q", updated.Status.LastError)
			}
			if !tc.expectOK && updated.Status.LastError == "" {
				t.Fatalf("expected non-empty lastError when unhealthy")
			}
			if updated.Status.LastHealthCheckTime == nil {
				t.Fatalf("expected LastHealthCheckTime to be set")
			}
		})
	}
}
