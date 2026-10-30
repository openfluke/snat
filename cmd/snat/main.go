// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 OpenFluke contributors.

// Command snat executes a single JSON library request from stdin.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/openfluke/snat"
)

func main() {
	root := flag.String("data-root", "", "Application data root (required; recordings use its neuralwave subfolder)")
	listen := flag.String("listen", "", "Serve SNAT TCP instead of reading one stdin request")
	flag.Parse()
	if *root == "" {
		fmt.Fprintln(os.Stderr, "snat: -data-root is required")
		os.Exit(2)
	}
	if *listen != "" {
		raw, _ := json.Marshal(snat.Request{Action: "server_start", Address: *listen, Token: os.Getenv("SNAT_TCP_TOKEN")})
		status, err := snat.Execute(*root, raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(status)
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop
		signal.Stop(stop)
		snat.StopLive(*root)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 64<<20+1))
	if err == nil && len(raw) > 64<<20 {
		err = fmt.Errorf("request exceeds 64 MiB")
	}
	var result any
	if err == nil {
		result, err = snat.Execute(*root, raw)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "snat:", err)
		os.Exit(1)
	}
	if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "snat:", err)
		os.Exit(1)
	}
}
