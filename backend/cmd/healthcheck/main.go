package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := client.Get("http://127.0.0.1:18080/readyz")
	if err != nil {
		os.Exit(1)
	}
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
