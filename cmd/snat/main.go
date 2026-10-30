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

	"github.com/openfluke/snat"
)

func main() {
	root := flag.String("data-root", "", "Application data root (required; recordings use its neuralwave subfolder)")
	flag.Parse()
	if *root == "" {
		fmt.Fprintln(os.Stderr, "snat: -data-root is required")
		os.Exit(2)
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
