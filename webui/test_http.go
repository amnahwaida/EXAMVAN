//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"net/http"
)

func main() {
	resp, err := http.Post("http://localhost:5001/admin/api/exams/1/stop", "application/json", nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	fmt.Printf("Status: %s\n", resp.Status)
}
