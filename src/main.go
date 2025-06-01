package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
)

type simpleServer struct {
	addr  string
	proxy *httputil.ReverseProxy
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
}

func handleErr(err error) {
	if err != nil {
		fmt.Println(err)
	}
}

func initialiseServer(addr string) *simpleServer {
	serverUrl, err := url.Parse(addr)
	handleErr(err)
	return &simpleServer{
		addr:  addr,
		proxy: httputil.NewSingleHostReverseProxy(serverUrl),
	}
}

func newLoadBalancer(servers []Server, port string, algo string) *LoadBalancer {
	return &LoadBalancer{
		servers:   servers,
		port:      port,
		algorithm: algo,
	}
}

func (s *simpleServer) Address() string { return s.addr }

func (s *simpleServer) isAlive() bool { return true }

func (s *simpleServer) Serve(rw http.ResponseWriter, req *http.Request) {
	s.proxy.ServeHTTP(rw, req)
}

// get next available server -> return a server
func (lb *LoadBalancer) getNextAvailableSever() Server {
	// random
	//server := lb.servers[rand.Intn(len(lb.servers))]
	//for server.isAlive() {
	//	server = lb.servers[rand.Intn(len(lb.servers))]
	//}
	//return server
	for {
		server := lb.servers[rand.Intn(len(lb.servers))]
		if server.isAlive() {
			return server
		}
	}
}

func (lb *LoadBalancer) serveProxy(rw http.ResponseWriter, req *http.Request) {
	targetServer := lb.getNextAvailableSever()
	fmt.Println("Serving request to ", targetServer.Address())
	targetServer.Serve(rw, req)
}

func main() {
	servers := []Server{
		initialiseServer("http://localhost:9000"),
		initialiseServer("http://localhost:9001"),
		initialiseServer("http://localhost:9002"),
	}

	lb := newLoadBalancer(servers, "8000", "roundrobin")

	handleRedirect := func(rw http.ResponseWriter, req *http.Request) {
		lb.serveProxy(rw, req)
	}

	http.HandleFunc("/", handleRedirect)

	fmt.Println("Starting server on port", lb.port)

	err := http.ListenAndServe(":"+lb.port, nil)
	handleErr(err)
}
