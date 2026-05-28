package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/httpx"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.WriteJSON(rec, http.StatusCreated, map[string]int{"n": 1})
	if rec.Code != http.StatusCreated {
		t.Errorf("code = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"n":1`) {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.WriteError(rec, http.StatusBadRequest, "bad_input", "nope")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d", rec.Code)
	}
	var env httpx.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error.Code != "bad_input" || env.Error.Message != "nope" {
		t.Errorf("envelope = %+v", env.Error)
	}
}

func TestWriteAPIErrorMapsMediapath(t *testing.T) {
	r, err := mediapath.NewResolver([]catalog.Library{{Alias: "pics", Name: "P", Root: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		raw        string
		wantStatus int
		wantCode   string
	}{
		{"unknown/x", http.StatusNotFound, "unknown_alias"},
		{"pics/../../etc", http.StatusBadRequest, "invalid_path"},
		{"", http.StatusBadRequest, "invalid_path"},
	}
	for _, tc := range cases {
		_, _, _, rerr := r.Resolve(catalog.MediaPath(tc.raw))
		rec := httptest.NewRecorder()
		httpx.WriteAPIError(rec, rerr)
		if rec.Code != tc.wantStatus {
			t.Errorf("%q: status = %d, want %d", tc.raw, rec.Code, tc.wantStatus)
		}
		var env httpx.ErrorEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		if env.Error.Code != tc.wantCode {
			t.Errorf("%q: code = %q, want %q", tc.raw, env.Error.Code, tc.wantCode)
		}
	}
}

func TestDecodeJSON(t *testing.T) {
	type body struct {
		Name string `json:"name"`
	}
	t.Run("valid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"x"}`))
		var b body
		if err := httpx.DecodeJSON(req, &b); err != nil {
			t.Fatal(err)
		}
		if b.Name != "x" {
			t.Errorf("name = %q", b.Name)
		}
	})
	t.Run("unknown field rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"bogus":1}`))
		var b body
		if err := httpx.DecodeJSON(req, &b); err == nil {
			t.Fatal("expected error for unknown field")
		}
	})
	t.Run("trailing content rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"x"}{}`))
		var b body
		if err := httpx.DecodeJSON(req, &b); err == nil {
			t.Fatal("expected error for trailing content")
		}
	})
}

func TestParseMediaPath(t *testing.T) {
	r, err := mediapath.NewResolver([]catalog.Library{{Alias: "pics", Name: "P", Root: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := httpx.ParseMediaPath(r, "pics/a.jpg"); err != nil {
		t.Errorf("valid path rejected: %v", err)
	}
	if _, err := httpx.ParseMediaPath(r, "pics/../x"); err == nil {
		t.Error("traversal path accepted")
	}
}
