package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if os.Getenv("MONO_TEST_FORWARD_BLOCK") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	for i, mapping := range os.Args[9:] {
		pair := strings.Split(mapping, ":")
		listener, err := net.Listen("tcp", "127.0.0.1:"+pair[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		go func() {
			_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
		}()
		if os.Getenv("MONO_TEST_FORWARD_PARTIAL") != "1" || i < 5 {
			fmt.Printf("Forwarding from 127.0.0.1:%s -> %s\n", pair[0], pair[1])
		}
	}
	for {
		time.Sleep(time.Hour)
	}
}
