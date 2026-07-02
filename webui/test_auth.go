package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
)

func main() {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// 1. Get login page to get CSRF
	resp, _ := client.Get("http://localhost:5001/admin/login")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	
	re := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	matches := re.FindStringSubmatch(string(body))
	csrf := matches[1]

	// 2. Login
	form := url.Values{}
	form.Add("username", "superadmin")
	form.Add("password", "examvan2026")
	form.Add("csrf_token", csrf)
	
	req, _ := http.NewRequest("POST", "http://localhost:5001/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp2, _ := client.Do(req)
	resp2.Body.Close()

	// 3. Get /admin/pengawas/1 to get API CSRF
	resp3, _ := client.Get("http://localhost:5001/admin/pengawas/1")
	body3, _ := io.ReadAll(resp3.Body)
	resp3.Body.Close()
	re2 := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)">`)
	matches2 := re2.FindStringSubmatch(string(body3))
	apiCsrf := matches2[1]

	// 4. Hit /admin/api/exams/1/stop
	req4, _ := http.NewRequest("POST", "http://localhost:5001/admin/api/exams/1/stop", nil)
	req4.Header.Set("X-CSRF-Token", apiCsrf)
	resp4, _ := client.Do(req4)
	body4, _ := io.ReadAll(resp4.Body)
	resp4.Body.Close()
	fmt.Printf("API Stop Status: %s\nBody: %s\n", resp4.Status, string(body4))
	
	// 5. Hit /admin/api/exams/1/start
	req5, _ := http.NewRequest("POST", "http://localhost:5001/admin/api/exams/1/start", nil)
	req5.Header.Set("X-CSRF-Token", apiCsrf)
	resp5, _ := client.Do(req5)
	body5, _ := io.ReadAll(resp5.Body)
	resp5.Body.Close()
	fmt.Printf("API Start Status: %s\nBody: %s\n", resp5.Status, string(body5))
}
