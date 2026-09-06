package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"notif-layer/pkg/check"
)

const interval = 30 * time.Minute

func main() {
	if err := loadDotEnv(".env"); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "error: gagal baca .env: %v\n", err)
		os.Exit(1)
	}

	once := len(os.Args) > 1 && os.Args[1] == "--once"
	for {
		runOnce()
		if once {
			return
		}
		next := time.Now().Add(interval).Format("15:04:05")
		fmt.Printf("Menunggu 30 menit... cek lagi pukul %s (Ctrl+C untuk berhenti)\n", next)
		time.Sleep(interval)
	}
}

func runOnce() {
	result, err := check.Run()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		_ = enc.Encode(result)
		return
	}
	_ = enc.Encode(result)
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
	return scanner.Err()
}
