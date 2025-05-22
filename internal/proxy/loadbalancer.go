package proxy

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/Aashi-001/Load-Balancer/loadbalancer/internal/config"
)

type Backend struct {
	URL          *url.URL
	ReverseProxy *httputil.ReverseProxy
}

type LoadBalancer struct {
	backends []*Backend
	current  uint64
}

func NewLoadBalancer(cfg *config.Config) (*LoadBalancer, error) {
	var backends []*Backend

	// Initialize backends
	for _, backendCfg := range cfg.Backends {
		serverURL, err := url.Parse(backendCfg.URL)
		if err != nil {
			return nil, err
		}

		backend := &Backend{
			URL:          serverURL,
			ReverseProxy: httputil.NewSingleHostReverseProxy(serverURL),
		}
		backends = append(backends, backend)
	}

	return &LoadBalancer{
		backends: backends,
	}, nil
}

func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Simple round-robin selection
	backend := lb.nextBackend()
	if backend == nil {
		http.Error(w, "No backends available", http.StatusServiceUnavailable)
		return
	}

	// Record start time
	start := time.Now()

	// Forward request to backend
	backend.ReverseProxy.ServeHTTP(w, r)

	// Log request
	duration := time.Since(start)
	log.Printf("%s %s -> %s (%v)", r.Method, r.URL.Path, backend.URL.Host, duration)
}

func (lb *LoadBalancer) nextBackend() *Backend {
	if len(lb.backends) == 0 {
		return nil
	}

	// Atomic counter for thread-safe round-robin
	next := atomic.AddUint64(&lb.current, 1)
	return lb.backends[next%uint64(len(lb.backends))]
}
