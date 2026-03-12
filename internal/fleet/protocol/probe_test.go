package protocol

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProbeHTTPRejects404OnlyResponses(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(req, http.StatusNotFound, "not found"), nil
	})}

	result := probeHTTP(client, "http://agent.example")
	if result.Healthy {
		t.Fatalf("expected 404-only responses to be rejected, got %+v", result)
	}
}

func TestProbeHTTPDetectsOpenAIModelMetadata(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v1/models":
			return newResponse(req, http.StatusOK, `{"data":[{"id":"demo-model"}]}`), nil
		case "/chat", "/invoke":
			return newResponse(req, http.StatusNotFound, "not found"), nil
		default:
			return newResponse(req, http.StatusNotFound, "not found"), nil
		}
	})}

	result := probeHTTP(client, "http://agent.example")
	if !result.Healthy {
		t.Fatalf("expected openai-compatible endpoint to probe healthy, got %+v", result)
	}
	if result.Metadata["chatPath"] != "/v1/chat/completions" {
		t.Fatalf("expected openai chat path metadata, got %+v", result.Metadata)
	}
	if result.Metadata["model"] != "demo-model" {
		t.Fatalf("expected discovered model metadata, got %+v", result.Metadata)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func newResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}
}
