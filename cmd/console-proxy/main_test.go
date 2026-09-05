/*
Copyright 2026 The Soteria Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

func generateSelfSignedCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// --- healthz ---

func TestHandleHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	handleHealthz(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("expected body 'ok', got %q", string(body))
	}
}

// --- SPA handler ---

func TestSpaHandler_ServesStaticFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "static", "js"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "static", "js", "main.js"), []byte("console.log('ok')"), 0o644)

	handler := spaHandler(dir)

	req := httptest.NewRequest(http.MethodGet, "/static/js/main.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "console.log('ok')" {
		t.Fatalf("unexpected body: %q", string(body))
	}
}

func TestSpaHandler_FallbackToIndex(t *testing.T) {
	dir := t.TempDir()
	indexContent := "<html>spa</html>"
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte(indexContent), 0o644)

	handler := spaHandler(dir)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/plan/my-plan", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != indexContent {
		t.Fatalf("expected index.html content, got %q", string(body))
	}
}

func TestSpaHandler_RootServesIndex(t *testing.T) {
	dir := t.TempDir()
	indexContent := "<html>root</html>"
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte(indexContent), 0o644)

	handler := spaHandler(dir)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != indexContent {
		t.Fatalf("expected index.html content, got %q", string(body))
	}
}

// --- K8s proxy construction ---

func TestNewK8sProxy_MissingToken(t *testing.T) {
	_, err := newK8sProxyWithPaths(mustParseURL("https://10.0.0.1:443"), "/nonexistent/token", "/nonexistent/ca.crt")
	if err == nil {
		t.Fatal("expected error when SA token file is missing")
	}
}

func TestNewK8sProxy_MissingCA(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	_ = os.WriteFile(tokenFile, []byte("test-token"), 0o644)

	_, err := newK8sProxyWithPaths(mustParseURL("https://10.0.0.1:443"), tokenFile, "/nonexistent/ca.crt")
	if err == nil {
		t.Fatal("expected error when CA file is missing")
	}
}

func TestNewK8sProxy_InvalidCA(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	caFile := filepath.Join(dir, "ca.crt")
	_ = os.WriteFile(tokenFile, []byte("test-token"), 0o644)
	_ = os.WriteFile(caFile, []byte("not-a-cert"), 0o644)

	_, err := newK8sProxyWithPaths(mustParseURL("https://10.0.0.1:443"), tokenFile, caFile)
	if err == nil {
		t.Fatal("expected error for invalid CA cert")
	}
}

func TestNewK8sProxy_Success(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	caFile := filepath.Join(dir, "ca.crt")

	_ = os.WriteFile(tokenFile, []byte("test-token"), 0o644)
	_ = os.WriteFile(caFile, generateSelfSignedCAPEM(t), 0o644)

	proxy, err := newK8sProxyWithPaths(mustParseURL("https://10.0.0.1:443"), tokenFile, caFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proxy == nil {
		t.Fatal("proxy should not be nil")
	}
}

// --- Token injection & path rewrite ---

func TestNewK8sProxy_InjectsTokenAndRewritesPath(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	caFile := filepath.Join(dir, "ca.crt")

	_ = os.WriteFile(tokenFile, []byte("my-sa-token"), 0o644)
	_ = os.WriteFile(caFile, generateSelfSignedCAPEM(t), 0o644)

	var capturedAuth string
	var capturedPath string
	backend := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedPath = r.URL.Path
	}))
	defer backend.Close()

	backendCA := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: backend.TLS.Certificates[0].Certificate[0],
	})
	_ = os.WriteFile(caFile, backendCA, 0o644)

	proxy, err := newK8sProxyWithPaths(mustParseURL(backend.URL), tokenFile, caFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle(k8sAPIPrefix, proxy)

	req := httptest.NewRequest(http.MethodGet, "/api/k8s/apis/soteria.io/v1alpha1/drplans", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if capturedAuth != "Bearer my-sa-token" {
		t.Fatalf("expected token injection, got Authorization: %q", capturedAuth)
	}
	if capturedPath != "/apis/soteria.io/v1alpha1/drplans" {
		t.Fatalf("expected path rewrite, got %q", capturedPath)
	}
}

// --- Token refresh ---

func TestTokenReader_RefreshesFromDisk(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	_ = os.WriteFile(tokenFile, []byte("initial-token"), 0o644)

	tr, err := newTokenReader(tokenFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Token() != "initial-token" {
		t.Fatalf("expected initial-token, got %q", tr.Token())
	}

	_ = os.WriteFile(tokenFile, []byte("rotated-token"), 0o644)
	if err := tr.refresh(); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if tr.Token() != "rotated-token" {
		t.Fatalf("expected rotated-token, got %q", tr.Token())
	}
}

// --- Core API path rewrite ---

func TestNewK8sProxy_CoreAPIPathRewrite(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	caFile := filepath.Join(dir, "ca.crt")

	_ = os.WriteFile(tokenFile, []byte("my-sa-token"), 0o644)

	var capturedPath string
	backend := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
	}))
	defer backend.Close()

	backendCA := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: backend.TLS.Certificates[0].Certificate[0],
	})
	_ = os.WriteFile(caFile, backendCA, 0o644)

	proxy, err := newK8sProxyWithPaths(mustParseURL(backend.URL), tokenFile, caFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle(k8sAPIPrefix, proxy)

	req := httptest.NewRequest(http.MethodGet, "/api/k8s/api/v1/pods", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if capturedPath != "/api/v1/pods" {
		t.Fatalf("expected core API path /api/v1/pods, got %q", capturedPath)
	}
}
