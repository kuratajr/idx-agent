package main

import (
	"fmt"
	"net"
	"os"
	"testing"
)

func TestLookupIP(t *testing.T) {
	if ci := os.Getenv("CI"); ci != "" { // skip if test on CI
		return
	}

	// Avoid external DNS dependency; just ensure we can pass through IPs and resolve localhost.
	ip, err := lookupIP("127.0.0.1")
	fmt.Printf("ip: %v, err: %v\n", ip, err)
	if err != nil || ip != "127.0.0.1" {
		t.Fatalf("lookupIP for literal IP failed: ip=%q err=%v", ip, err)
	}

	ip, err = lookupIP("localhost")
	fmt.Printf("ip: %v, err: %v\n", ip, err)
	if err != nil {
		t.Fatalf("lookupIP localhost failed: %v", err)
	}
	if _, err := net.ResolveIPAddr("ip", "localhost"); err != nil {
		t.Fatalf("ResolveIPAddr localhost failed: %v", err)
	}
}
