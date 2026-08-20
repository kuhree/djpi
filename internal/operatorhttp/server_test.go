package operatorhttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.kuhree.com/kuhree.com/djpi/internal/status"
)

func TestReadOnlyOperatorEndpoints(t *testing.T) {
	runtime := status.RuntimeStatus{State: "ingesting", StartedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC(), Files: 9}
	wanted := status.Report{Time: time.Unix(1, 0).UTC(), Runtime: &runtime}
	handler := Handler(func() (status.Report, error) { return wanted, nil })

	for _, test := range []struct {
		path        string
		contentType string
	}{
		{path: "/", contentType: "text/html"},
		{path: "/healthz", contentType: "application/json"},
		{path: "/readyz", contentType: "application/json"},
		{path: "/api/v1/status", contentType: "application/json"},
	} {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d", response.Code)
			}
			if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, test.contentType) {
				t.Fatalf("content type = %q", got)
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Security-Policy") == "" {
				t.Fatalf("missing safety headers: %#v", response.Header())
			}
		})
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	var got status.Report
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Time.Equal(wanted.Time) || got.Runtime == nil || got.Runtime.State != "ingesting" || got.Runtime.Files != 9 {
		t.Fatalf("report = %#v", got)
	}
}

func TestStatusFailureDoesNotLeakError(t *testing.T) {
	handler := Handler(func() (status.Report, error) { return status.Report{}, errors.New("secret path") })
	for _, endpoint := range []string{"/readyz", "/api/v1/status"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if response.Code < 500 || strings.Contains(response.Body.String(), "secret path") {
			t.Fatalf("%s response = %d %q", endpoint, response.Code, response.Body.String())
		}
	}
}

func TestOperatorSurfaceHasNoMutationRoutes(t *testing.T) {
	handler := Handler(func() (status.Report, error) { return status.Report{}, nil })
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/status", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", response.Code)
	}
}
