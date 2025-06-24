package main

import (
	"fmt"
	"net/http"
	"sync"
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

func main() {
	ports := []int{9000, 9001, 9002}
	var wg sync.WaitGroup

	for _, port := range ports {
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
	wg.Wait()
}
