package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRFGuard(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	guarded := csrfGuard(next)

	cases := []struct {
		name       string
		method     string
		path       string
		headers    map[string]string
		wantStatus int
	}{
		{
			"cross-site POST blocked",
			http.MethodPost,
			"/v1/providers/x/test",
			map[string]string{"Sec-Fetch-Site": "cross-site"},
			http.StatusForbidden,
		},
		{
			"cross-origin POST blocked",
			http.MethodPost,
			"/v1/providers",
			map[string]string{"Origin": "http://evil.example.com"},
			http.StatusForbidden,
		},
		{
			"same-origin POST allowed",
			http.MethodPost,
			"/v1/providers",
			map[string]string{"Sec-Fetch-Site": "same-origin"},
			http.StatusOK,
		},
		{
			"same-origin Origin allowed",
			http.MethodPost,
			"/v1/providers",
			map[string]string{"Origin": "http://localhost:8083"},
			http.StatusOK,
		},
		{
			"no metadata POST allowed (local tooling)",
			http.MethodPost,
			"/v1/providers",
			nil,
			http.StatusOK,
		},
		{
			"cross-site GET allowed (read-only)",
			http.MethodGet,
			"/v1/events",
			map[string]string{"Sec-Fetch-Site": "cross-site"},
			http.StatusOK,
		},
		{
			"cross-site DELETE blocked",
			http.MethodDelete,
			"/v1/providers/x",
			map[string]string{"Sec-Fetch-Site": "cross-site"},
			http.StatusForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Host = "localhost:8083"
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Errorf("%s: status = %d, want %d", tc.name, rec.Code, tc.wantStatus)
			}
		})
	}
}
