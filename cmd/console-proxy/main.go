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
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultAddr      = ":8080"
	defaultStaticDir = "/opt/app-root/src"
	tokenPath        = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	caPath           = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	k8sAPIPrefix     = "/api/k8s/"
)

func main() {
	addr := flag.String("addr", defaultAddr, "Listen address")
	staticDir := flag.String("static-dir", defaultStaticDir, "Directory with static SPA files")
	flag.Parse()

	k8sHost := os.Getenv("KUBERNETES_SERVICE_HOST")
	k8sPort := os.Getenv("KUBERNETES_SERVICE_PORT")
	if k8sHost == "" || k8sPort == "" {
		log.Fatal("KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT must be set")
	}

	hostPort := net.JoinHostPort(k8sHost, k8sPort)
	k8sURL, err := url.Parse("https://" + hostPort)
	if err != nil {
		log.Fatalf("Failed to parse K8s API URL: %v", err)
	}

	proxy, err := newK8sProxy(k8sURL)
	if err != nil {
		log.Fatalf("Failed to create K8s proxy: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.Handle(k8sAPIPrefix, proxy)
	mux.Handle("/", spaHandler(*staticDir))

	srv := &http.Server{
		Addr:         *addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("Listening on %s, static from %s, proxying to %s", *addr, *staticDir, k8sURL)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit
	log.Println("Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server stopped")
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func newK8sProxy(target *url.URL) (http.Handler, error) {
	return newK8sProxyWithPaths(target, tokenPath, caPath)
}

// tokenReader caches the SA token and refreshes it periodically to handle
// projected token rotation (default ~1h lifetime).
type tokenReader struct {
	path  string
	mu    sync.RWMutex
	token string
}

func newTokenReader(path string) (*tokenReader, error) {
	tr := &tokenReader{path: path}
	if err := tr.refresh(); err != nil {
		return nil, err
	}
	go tr.refreshLoop()
	return tr, nil
}

func (tr *tokenReader) refresh() error {
	data, err := os.ReadFile(tr.path)
	if err != nil {
		return fmt.Errorf("read SA token: %w", err)
	}
	tr.mu.Lock()
	tr.token = string(data)
	tr.mu.Unlock()
	return nil
}

func (tr *tokenReader) refreshLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := tr.refresh(); err != nil {
			log.Printf("Warning: failed to refresh SA token: %v", err)
		}
	}
}

func (tr *tokenReader) Token() string {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return tr.token
}

func newK8sProxyWithPaths(target *url.URL, saTokenPath, saCaPath string) (http.Handler, error) {
	tr, err := newTokenReader(saTokenPath)
	if err != nil {
		return nil, err
	}

	caCert, err := os.ReadFile(saCaPath)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    caPool,
			MinVersion: tls.VersionTLS12,
		},
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = strings.TrimPrefix(req.URL.Path, "/api/k8s")
			req.Header.Set("Authorization", "Bearer "+tr.Token())
			req.Host = target.Host
		},
		Transport: transport,
	}

	return proxy, nil
}

func spaHandler(staticDir string) http.Handler {
	fs := http.Dir(staticDir)
	fileServer := http.FileServer(fs)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := filepath.Clean(r.URL.Path)
		if cleanPath == "/" {
			cleanPath = "/index.html"
		}

		f, err := fs.Open(cleanPath)
		if err != nil {
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, r)
	})
}
