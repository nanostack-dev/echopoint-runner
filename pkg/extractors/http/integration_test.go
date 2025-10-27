package http

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
)

// TestNewInterfaceDesignPattern demonstrates the new interface-based extractor pattern
func TestNewInterfaceDesignPattern(t *testing.T) {
	// Create a test HTTP server that returns JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "test-value")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user": map[string]interface{}{
				"id":   123,
				"name": "John Doe",
			},
		})
	}))
	defer server.Close()

	// Make the request
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	// Parse response
	var parsedBody interface{}
	if err := json.Unmarshal(respBody, &parsedBody); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Create ResponseContext - this is the KEY improvement
	// The context encapsulates all available data from the response
	ctx := extractors.NewResponseContext(resp, respBody, parsedBody)

	t.Run("StatusCodeExtractor declares it needs StatusReader interface", func(t *testing.T) {
		extractor := &StatusCodeExtractor{}
		result, err := extractor.Extract(ctx)
		if err != nil {
			t.Fatalf("Extraction failed: %v", err)
		}

		statusCode, ok := result.(int)
		if !ok || statusCode != http.StatusCreated {
			t.Errorf("Expected status %d, got %v", http.StatusCreated, result)
		}
	})

	t.Run("HeaderExtractor declares it needs HeaderAccessor interface", func(t *testing.T) {
		extractor := &HeaderExtractor{HeaderName: "X-Custom-Header"}
		result, err := extractor.Extract(ctx)
		if err != nil {
			t.Fatalf("Extraction failed: %v", err)
		}

		value, ok := result.(string)
		if !ok || value != "test-value" {
			t.Errorf("Expected 'test-value', got %v", result)
		}
	})

	t.Run("Context provides capability checking", func(t *testing.T) {
		// The context can report what capabilities it provides
		if !ctx.HasCapability("status") {
			t.Error("Context should have 'status' capability")
		}
		if !ctx.HasCapability("headers") {
			t.Error("Context should have 'headers' capability")
		}
		if !ctx.HasCapability("parsed_body") {
			t.Error("Context should have 'parsed_body' capability")
		}
		if ctx.HasCapability("timing") {
			t.Error("Context should not have 'timing' capability yet")
		}
	})

	t.Run("Type assertions enforce explicit dependencies", func(t *testing.T) {
		// StatusReader interface
		sr, ok := ctx.(extractors.StatusReader)
		if !ok {
			t.Fatal("Context should implement StatusReader")
		}
		if sr.GetStatus() != http.StatusCreated {
			t.Errorf("Expected status %d", http.StatusCreated)
		}

		// HeaderAccessor interface
		ha, ok := ctx.(extractors.HeaderAccessor)
		if !ok {
			t.Fatal("Context should implement HeaderAccessor")
		}
		if ha.GetHeader("X-Custom-Header") != "test-value" {
			t.Error("Header value mismatch")
		}

		// ParsedBodyReader interface
		pbr, ok := ctx.(extractors.ParsedBodyReader)
		if !ok {
			t.Fatal("Context should implement ParsedBodyReader")
		}
		if pbr.GetParsedBody() == nil {
			t.Error("Parsed body should not be nil")
		}
	})
}

// BenchmarkNewDesign shows the performance characteristics of the new pattern
func BenchmarkNewDesign(b *testing.B) {
	// Setup
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "benchmark-value")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": "test",
		})
	}))
	defer server.Close()

	resp, _ := http.Get(server.URL)
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var parsedBody interface{}
	json.Unmarshal(respBody, &parsedBody)

	ctx := extractors.NewResponseContext(resp, respBody, parsedBody)

	statusExtractor := &StatusCodeExtractor{}
	headerExtractor := &HeaderExtractor{HeaderName: "X-Custom-Header"}

	b.Run("StatusCodeExtractor-ExecutionTime", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			statusExtractor.Extract(ctx)
		}
	})

	b.Run("HeaderExtractor-ExecutionTime", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			headerExtractor.Extract(ctx)
		}
	})

	b.Run("InterfaceTypeAssertion", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = ctx.(extractors.StatusReader)
			_, _ = ctx.(extractors.HeaderAccessor)
		}
	})
}
