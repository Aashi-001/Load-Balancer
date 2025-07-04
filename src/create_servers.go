package main

import (
	"fmt"
	"net/http"
	"sync"
	"strconv"
	"strings"
)

func handler(port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		case "/":
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "Hello from port %d", port)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func extractPort(url string) (int, error) {
	colonIndex := strings.LastIndex(url, ":")
	if colonIndex == -1 || colonIndex+1 >= len(url) {
		return 0, fmt.Errorf("invalid url format: %s", url)
	}
	portStr := url[colonIndex+1:]
	return strconv.Atoi(portStr)
}

func create_servers() {
	// ports := []int{9000, 9001, 9002}
	cfg := loadConfig("../configs/config.yaml")
	ports := cfg.Backends;
	var strports []int 
	for _, port := range ports {
		// println(port);
		// err := nil
		strport, err := extractPort(port)
		if(err != nil){
			println("something went wrong")
			return ;
		}
		strports = append(strports, strport)
	}
	var wg sync.WaitGroup

	for _, port := range strports {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			mux := http.NewServeMux()
			mux.HandleFunc("/", handler(p))
			serverAddr := fmt.Sprintf("localhost:%d", p)
			fmt.Printf("Serving on port %d\n", p)
			err := http.ListenAndServe(serverAddr, mux)
			if err != nil {
				fmt.Printf("Error on port %d: %v\n", p, err)
			}
		}(port)
	}

	fmt.Println("Press Ctrl+C to exit...")
	// wg.Wait()
}
