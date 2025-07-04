package main

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"gopkg.in/yaml.v2"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

// import "os"
var db *sql.DB

var (
    requestsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "lb_requests_total",
            Help: "Total number of requests processed by the load balancer",
        },
        []string{"backend", "algorithm", "status"},
    )
    
    responseTime = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "lb_response_duration_seconds",
            Help: "Response time distribution",
            Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
        },
        []string{"backend", "algorithm"},
    )
    
    activeConnections = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "lb_active_connections",
            Help: "Number of active connections per backend",
        },
        []string{"backend"},
    )
    
    backendHealth = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "lb_backend_health",
            Help: "Health status of backends (1=healthy, 0=unhealthy)",
        },
        []string{"backend"},
    )
)

type Config struct {
	Server struct {
		Host string `yaml:"host"`
		Port string    `yaml:"port"`
	} `yaml:"server"`

	Backends    []string `yaml:"backends"`
	Algorithm   string   `yaml:"algorithm"`
	HealthCheck struct {
		Interval time.Duration `yaml:"interval"`
		Timeout  time.Duration `yaml:"timeout"`
		Path     string        `yaml:"path"`
	} `yaml:"health_check"`
}

type simpleServer struct {
	addr        string
	proxy       *httputil.ReverseProxy
	activeConns int32
	alive       bool
	mu          sync.RWMutex
	requests    int64
}

type Server interface {
	Address() string
	isAlive() bool
	Serve(rw http.ResponseWriter, r *http.Request)
}


type LoadBalancer struct {
	servers   []Server
	port      string
	algorithm string
	mu        sync.RWMutex
}

type loggingResponseWriter struct {
    http.ResponseWriter
    statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
    lrw.statusCode = code
    lrw.ResponseWriter.WriteHeader(code)
}


func setupDB(path string) *sql.DB {
    db, err := sql.Open("sqlite3", path)
    if err != nil {
        log.Fatalf("Failed to open DB: %v", err)
    }

    schema := `
    CREATE TABLE IF NOT EXISTS request_logs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
        client_ip TEXT,
        path TEXT,
        backend TEXT,
        response_time_ms INTEGER,
        status_code INTEGER
    );
    CREATE TABLE IF NOT EXISTS health_check_logs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
        backend TEXT,
        is_alive BOOLEAN,
        response_time_ms INTEGER
    );
    `

    _, err = db.Exec(schema)
    if err != nil {
        log.Fatalf("Failed to initialize schema: %v", err)
    }

    return db
}

func logRequest(ip, path, backend string, respTime int64, status int) {
    _, err := db.Exec(`INSERT INTO request_logs (client_ip, path, backend, response_time_ms, status_code)
                       VALUES (?, ?, ?, ?, ?)`, ip, path, backend, respTime, status)
    if err != nil {
        log.Printf("DB insert failed: %v", err)
    }
}

func logHealthCheck(backend string, alive bool, respTime int64) {
    _, err := db.Exec(`INSERT INTO health_check_logs (backend, is_alive, response_time_ms)
                       VALUES (?, ?, ?)`, backend, alive, respTime)
    if err != nil {
        log.Printf("DB insert failed: %v", err)
    }
}


func handleErr(err error) {
	if err != nil {
		fmt.Println(err)
	}
}

func (s *simpleServer) increment() {
	// atomic.AddInt32(&s.activeConns, 1)
	atomic.AddInt32(&s.activeConns, 1)
	atomic.AddInt64(&s.requests, 1)
	activeConnections.WithLabelValues(s.addr).Set(float64(s.getActiveConns()))
}

func (s *simpleServer) decrement() {
	atomic.AddInt32(&s.activeConns, -1)
	activeConnections.WithLabelValues(s.addr).Set(float64(s.getActiveConns()))
}

func (s *simpleServer) getActiveConns() int32 {
	return atomic.LoadInt32(&s.activeConns)
}

func (s *simpleServer) Address() string { return s.addr }

// func (s *simpleServer) isAlive() bool { return true }
func (s *simpleServer) isAlive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.alive
}

func (s *simpleServer) checkHealth() {
	start := time.Now()
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(s.addr + "/health")
	duration := time.Since(start).Milliseconds()

	s.mu.Lock()
	
	// defer s.mu.Unlock()
	
	if err != nil || resp.StatusCode != http.StatusOK {
		s.alive = false
		logHealthCheck(s.addr, false, duration)
		backendHealth.WithLabelValues(s.addr).Set(0)
	} else {
		s.alive = true
		logHealthCheck(s.addr, true, duration)
		backendHealth.WithLabelValues(s.addr).Set(1)
		resp.Body.Close()
	}
	s.mu.Unlock()
}

func initialiseServer(addr string) *simpleServer {
	serverUrl, err := url.Parse(addr)
	handleErr(err)
	return &simpleServer{
		addr:  addr,
		proxy: httputil.NewSingleHostReverseProxy(serverUrl),
		alive: true,
	}
}

func newLoadBalancer(servers []Server, port string, algo string) *LoadBalancer {
	return &LoadBalancer{
		servers:   servers,
		port:      port,
		algorithm: algo,
	}
}

var rrCounter int32

func (lb *LoadBalancer) getNextRoundRobinServer() Server {
	total := len(lb.servers)
    if total == 0 {
        return nil
    }
    
    start := int(atomic.AddInt32(&rrCounter, 1) % int32(total))
    
    for i := range total {
        index := (start + i) % total
        if lb.servers[index].isAlive() {
            return lb.servers[index]
        }
    }
    return nil
}

func (lb *LoadBalancer) getLeastConnServer() Server {
	// minConn := int32(^uint32(0) >> 1) // max int
	// // var selected Server
	// var selected *simpleServer
	// // for _, s := range lb.servers {
	// // 	if s.isAlive() {
	// // 		ss := s.(*simpleServer)
	// // 		println(ss.activeConns)
	// // 		if ss.activeConns < minConn {
	// // 			minConn = ss.activeConns
	// // 			selected = ss
	// // 		}
	// // 	}
	// // }
	// for _, s := range lb.servers {
	// 	if s.isAlive() {
	// 		ss := s.(*simpleServer)
	// 		println(ss.getActiveConns())
	// 		curr := ss.getActiveConns()
	// 		if curr < minConn {
	// 			minConn = curr
	// 			selected = ss
	// 		}
	// 	}
	// }

	// if selected != nil {
	// 	selected.increment()
	// }

	// return selected
	var selected *simpleServer
	min := int32(^uint32(0) >> 1)
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	for _, srv := range lb.servers {
		if srv.isAlive() {
			ss := srv.(*simpleServer)
			if c := ss.getActiveConns(); c < min {
				selected = ss
				min = c
			}
		}
	}

	for _, srv := range lb.servers {
		ss := srv.(*simpleServer)
		fmt.Printf("Server: %s | Active: %d | Alive: %v\n", ss.addr, ss.getActiveConns(), ss.isAlive())
	}


	return selected
}

func (lb *LoadBalancer) getRandomServer() Server {
	for {
		server := lb.servers[rand.Intn(len(lb.servers))]
		if server.isAlive() {
			return server
		}
	}
}

func (lb *LoadBalancer) getNextAvailableSever() Server {
	println(lb.algorithm)
	switch lb.algorithm {
	case "roundrobin":
		return lb.getNextRoundRobinServer()
	case "leastconn":
		return lb.getLeastConnServer()
	default:
		return lb.getRandomServer()
	}
}

func (s *simpleServer) Serve(rw http.ResponseWriter, req *http.Request) {
	s.increment()
	// defer s.decrement()
	defer func() {
		s.decrement()
		fmt.Println("Decremented:", s.getActiveConns())
	}()
	s.proxy.ServeHTTP(rw, req)
}

// get next available server -> return a server
// func (lb *LoadBalancer) getNextAvailableSever() Server {
// 	// random
// 	//server := lb.servers[rand.Intn(len(lb.servers))]
// 	//for server.isAlive() {
// 	//	server = lb.servers[rand.Intn(len(lb.servers))]
// 	//}
// 	//return server
// 	for {
// 		server := lb.servers[rand.Intn(len(lb.servers))]
// 		if server.isAlive() {
// 			return server
// 		}
// 	}
// }


func init() {
    prometheus.MustRegister(requestsTotal)
    prometheus.MustRegister(responseTime)
    prometheus.MustRegister(activeConnections)
    prometheus.MustRegister(backendHealth)
}

func (lb *LoadBalancer) serveProxy(rw http.ResponseWriter, req *http.Request) {
	start := time.Now()
	log.Println("Incoming request")
	targetServer := lb.getNextAvailableSever()
	if targetServer == nil {
		log.Println("No alive servers available!")
		http.Error(rw, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
	fmt.Println("Serving request to ", targetServer.Address())

	lrw := &loggingResponseWriter{ResponseWriter: rw, statusCode: http.StatusOK}

	targetAddr := targetServer.Address()

	targetServer.Serve(lrw, req)

	duration := time.Since(start)
    logRequest(req.RemoteAddr, req.URL.Path, targetAddr, duration.Milliseconds(), lrw.statusCode)

	requestsTotal.WithLabelValues(targetAddr, lb.algorithm, fmt.Sprintf("%d", lrw.statusCode)).Inc()
    responseTime.WithLabelValues(targetAddr, lb.algorithm).Observe(duration.Seconds())
}

func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func loadConfig(path string) Config {
	data, err := readFile(path)
	if err != nil {
		log.Fatalf("Cannot read config file: %v", err)
	}
	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		log.Fatalf("Invalid config YAML: %v", err)
	}
	return cfg
}

func main() {
	// servers := []Server{
	// 	initialiseServer("http://localhost:9000"),
	// 	initialiseServer("http://localhost:9001"),
	// 	initialiseServer("http://localhost:9002"),
	// }

	// lb := newLoadBalancer(servers, "8000", "leastconn")

	// handleRedirect := func(rw http.ResponseWriter, req *http.Request) {
	// 	lb.serveProxy(rw, req)
	// }

	// http.HandleFunc("/", handleRedirect)

	// fmt.Println("Starting server on port", lb.port)

	// err := http.ListenAndServe(":"+lb.port, nil)
	// handleErr(err)

	create_servers();

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	// println("reading config")
	cfg := loadConfig("../configs/config.yaml")
	// println("done")
	fmt.Println(cfg.Server.Host)
	fmt.Println(cfg.HealthCheck.Interval)

	// println("initialising db")
	db = setupDB("lb_logs.db")
	// println("done")

	var servers []Server
	for _, addr := range cfg.Backends {
		servers = append(servers, initialiseServer(addr))
	}

	lb := newLoadBalancer(servers, cfg.Server.Port, cfg.Algorithm)

	go func() {
		for {
			for _, s := range lb.servers {
				go s.(*simpleServer).checkHealth()
			}
			time.Sleep(5 * time.Second)
		}
	}()

	go func() {
        http.Handle("/metrics", promhttp.Handler())
        log.Println("Metrics server starting on :2112")
        if err := http.ListenAndServe(":2112", nil); err != nil {
            log.Printf("Metrics server failed: %v", err)
        }
    }()

	http.HandleFunc("/", lb.serveProxy)

	log.Printf("Starting load balancer on port %s using %s algorithm\n", cfg.Server.Port, cfg.Algorithm)
	err := http.ListenAndServe(":"+ cfg.Server.Port, nil)
	if err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
