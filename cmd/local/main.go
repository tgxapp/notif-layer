package main

import (
	"encoding/json"
	"fmt"
	"os"

	"notif-layer/pkg/check"
)

func main() {
	result, err := check.Run()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		_ = enc.Encode(result)
		os.Exit(1)
	}
	_ = enc.Encode(result)
}
