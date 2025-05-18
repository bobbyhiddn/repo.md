//go:build js && wasm
// +build js,wasm

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"syscall/js"
	"time"
)

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

const MAX_FILE_SIZE = 1 * 1024 * 1024 // 1MB

type GitHubItem struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"` // Full path from repo root
	Type        string  `json:"type"`
	DownloadURL *string `json:"download_url"` // GitHub URL for raw content download
	APIURL      string  `json:"url"`          // GitHub API URL for item details or dir contents
	Size        int64   `json:"size"`
}

type Result struct {
	RepoName string `json:"repo_name"`
	Markdown string `json:"markdown"`
}

// getAppBaseURL returns the base URL for API requests.
// In development, it returns the full backend URL; in production, it returns an empty string for relative URLs.
func getAppBaseURL() string {
	// Check if we're running in a browser environment
	window := js.Global().Get("window")
	if window.IsUndefined() {
		return "http://localhost:8081" // Fallback for non-browser environments
	}
	
	// Get the current location
	location := window.Get("location")
	hostname := location.Get("hostname").String()
	
	// Local development - use the backend port (8081 in docker-compose)
	if hostname == "localhost" || hostname == "127.0.0.1" {
		return "http://localhost:8081"
	}
	
	// Production - use an empty string for relative URLs
	// This ensures the API requests go to the same server as the frontend
	js.Global().Get("console").Call("log", "Using relative URLs for API requests in production")
	return ""
}

func main() {
	js.Global().Set("generateMarkdown", js.FuncOf(generateMarkdown))
	<-make(chan bool)
}

func generateMarkdown(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		js.Global().Get("console").Call("error", "generateMarkdown called without a callback function")
		return nil
	}
	callback := args[1]

	go func() {
		var result Result
		if len(args) < 1 || args[0].IsNull() || args[0].IsUndefined() {
			result = Result{Markdown: "Error: URL argument is missing"}
			sendResult(callback, result)
			return
		}
		originalURL := args[0].String()
		if !strings.Contains(originalURL, "github.com") {
			result = Result{Markdown: "Error: Invalid GitHub URL. Must contain 'github.com'."}
			sendResult(callback, result)
			return
		}

		parts := strings.Split(strings.TrimSuffix(originalURL, "/"), "/")
		if len(parts) < 2 {
			result = Result{Markdown: "Error: Invalid GitHub URL format. Expected github.com/owner/repo"}
			sendResult(callback, result)
			return
		}
		owner := parts[len(parts)-2]
		repo := parts[len(parts)-1]
		githubRootAPIURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents", owner, repo)

		timestamp := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
		var markdownBuilder strings.Builder
		markdownBuilder.WriteString(fmt.Sprintf("# Repository: %s\nURL: %s\nTranscription Date: %s\n\n", repo, originalURL, timestamp))

		js.Global().Get("console").Call("log", "Starting processDirectoryContents via proxy for GitHub API URL: ", githubRootAPIURL)
		const maxRecursionDepth = 50
		// Initial call for root directory, currentPath is empty string for root
		err := processDirectoryContents(githubRootAPIURL, "", &markdownBuilder, 0, callback, maxRecursionDepth)

		if err != nil {
			js.Global().Get("console").Call("error", "Error from processDirectoryContents: ", err.Error())
			// Error messages (e.g. rate limit) are typically added to markdownBuilder within processDirectoryContents or its callees.
			// If not already specific, a general message could be added, but prefer specific ones.
			if !strings.Contains(markdownBuilder.String(), "Rate Limit Exceeded") && !strings.Contains(markdownBuilder.String(), "Error: Proxy returned status") {
				markdownBuilder.WriteString(fmt.Sprintf("\n\n--- ERROR DURING PROCESSING ---\n%v\n-----------------------------\n", err))
			}
		}

		markdownBuilder.WriteString("\n\n<!-- Generated with repo.md (https://repo-md.com) -->")
		result = Result{
			RepoName: repo,
			Markdown: markdownBuilder.String(),
		}
		sendResult(callback, result)
	}()
	return nil
}

// makeRequestWithRetries performs an HTTP request with retry logic for rate limiting
func makeRequestWithRetries(req *http.Request, client *http.Client, maxRetries int) (*http.Response, error) {
	var resp *http.Response
	var err error

	for i := 0; i <= maxRetries; i++ {
		resp, err = client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != 429 {
			// Success or error other than rate limiting
			return resp, nil
		}

		// Handle rate limiting (status 429)
		if i < maxRetries {
			// Close the response body to avoid resource leak
			resp.Body.Close()

			// Wait before retry (exponential backoff)
			waitTime := time.Duration(math.Pow(2, float64(i))) * time.Second
			js.Global().Get("console").Call("log", fmt.Sprintf("Rate limited. Retrying in %v seconds...", waitTime.Seconds()))
			time.Sleep(waitTime)

			// Create a fresh request for retry
			req, err = http.NewRequest(req.Method, req.URL.String(), nil)
			if err != nil {
				return nil, err
			}
		}
	}

	// If we reached here, we've exhausted all retries
	return resp, nil
}

func processDirectoryContents(githubDirectoryAPIURL string, currentItemPath string, markdown *strings.Builder, depth int, callback js.Value, maxRecursionDepth int) error {
	appBaseURL := getAppBaseURL()
	proxyDirectoryURL := fmt.Sprintf("%s/api/proxy_github_api?url=%s", appBaseURL, url.QueryEscape(githubDirectoryAPIURL))

	displayPath := currentItemPath
	if displayPath == "" {
		displayPath = "repository root"
	}
	js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Proxying dir request for '%s' to: %s (Original GitHub API URL: %s)", depth, displayPath, proxyDirectoryURL, githubDirectoryAPIURL))

	if depth > maxRecursionDepth {
		markdown.WriteString(fmt.Sprintf("\n*Skipping directory '%s' due to max recursion depth (%d).*\n", displayPath, maxRecursionDepth))
		return fmt.Errorf("max recursion depth %d reached for %s", maxRecursionDepth, displayPath)
	}

	req, err := http.NewRequest("GET", proxyDirectoryURL, nil)
	if err != nil {
		errMsg := fmt.Sprintf("\nError creating request for proxied directory '%s': %v\n", displayPath, err)
		markdown.WriteString(errMsg)
		return fmt.Errorf("http.NewRequest (proxy dir '%s'): %w", displayPath, err)
	}

	resp, err := makeRequestWithRetries(req, httpClient, 3) // Retry up to 3 times with exponential backoff
	if err != nil {
		errMsg := fmt.Sprintf("\nError fetching proxied directory contents for '%s' (from %s): %v\n", displayPath, proxyDirectoryURL, err)
		markdown.WriteString(errMsg)
		return fmt.Errorf("httpClient.Do (proxy dir '%s'): %w", displayPath, err)
	}
	defer resp.Body.Close()

	js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Proxy response for directory '%s'. Status: %d", depth, displayPath, resp.StatusCode))

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errorPayload := string(bodyBytes)
		js.Global().Get("console").Call("error", fmt.Sprintf("Proxy error for directory '%s': Status %d, Body: %s", displayPath, resp.StatusCode, errorPayload))

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == 429 { // 429 Too Many Requests
			marker := fmt.Sprintf("\n⚠️ GitHub API rate limit reached or authentication required. Please try again later or provide a GitHub token.\n\n")
			markdown.WriteString(marker)
			return fmt.Errorf("GitHub API rate limit or auth error: %d, %s", resp.StatusCode, errorPayload)
		}

		marker := fmt.Sprintf("Error response from GitHub API for '%s'. Status: %d\n\n", displayPath, resp.StatusCode)
		markdown.WriteString(marker)
		return fmt.Errorf("GitHub API error response: %d, %s", resp.StatusCode, errorPayload)
	}

	var items []GitHubItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		errMsg := fmt.Sprintf("\nError parsing proxied directory data for '%s' (from %s): %v\n", displayPath, proxyDirectoryURL, err)
		markdown.WriteString(errMsg)
		return fmt.Errorf("json.Decode (proxy dir '%s'): %w", displayPath, err)
	}

	if len(items) == 0 {
		if currentItemPath == "" { // Root directory
			markdown.WriteString(fmt.Sprintf("\n*Repository '%s' appears to be empty or contains no processable items via proxy.*\n", displayPath))
		} else {
			markdown.WriteString(fmt.Sprintf("\n*Directory `%s` is empty or contains no processable items via proxy.*\n", currentItemPath))
		}
	}

	for _, item := range items {
		// item.Path from GitHub API is the full path from the repo root.
		js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Processing item: '%s' (Type: %s, GitHub API URL: %s, GitHub DownloadURL: %v)", depth, item.Path, item.Type, item.APIURL, item.DownloadURL))

		if item.Type == "file" {
			markdown.WriteString(fmt.Sprintf("\n## /%s\n", item.Path))

			if item.Size > MAX_FILE_SIZE {
				markdown.WriteString(fmt.Sprintf("\n[File '%s' too large (%d bytes, limit %d bytes), content omitted.]\n\n", item.Path, item.Size, MAX_FILE_SIZE))
				continue
			}
			if item.Size == 0 {
				markdown.WriteString(fmt.Sprintf("```%s\n[Empty File: /%s]\n```\n\n", getFileExtension(item.Path), item.Path))
				continue
			}
			if item.DownloadURL == nil || *item.DownloadURL == "" {
				markdown.WriteString(fmt.Sprintf("\n[File content for '%s' not available (no download URL from GitHub API).]\n\n", item.Path))
				continue
			}

			githubFileRawURL := *item.DownloadURL
			proxyFileRawURL := fmt.Sprintf("%s/api/proxy_github_raw_content?url=%s", appBaseURL, url.QueryEscape(githubFileRawURL))

			js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Proxying raw file request for '%s' to: %s (Original GitHub Raw URL: %s)", depth, item.Path, proxyFileRawURL, githubFileRawURL))

			fileReq, fileReqErr := http.NewRequest("GET", proxyFileRawURL, nil)
			if fileReqErr != nil {
				markdown.WriteString(fmt.Sprintf("Error creating request for proxied file '%s': %v\n\n", item.Path, fileReqErr))
				continue
			}

			// Use retry mechanism for raw content requests
			fileResp, fileErr := makeRequestWithRetries(fileReq, httpClient, 3) // Retry up to 3 times with exponential backoff
			if fileErr != nil {
				markdown.WriteString(fmt.Sprintf("Error downloading proxied file '%s': %v\n\n", item.Path, fileErr))
				continue
			}
			defer fileResp.Body.Close()

			js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Proxy response for raw file '%s'. Status: %d", depth, item.Path, fileResp.StatusCode))

			if fileResp.StatusCode != http.StatusOK {
				fileBodyBytes, _ := io.ReadAll(fileResp.Body)
				fileResp.Body.Close()
				errorPayload := string(fileBodyBytes)
				js.Global().Get("console").Call("error", fmt.Sprintf("Proxy error for raw file '%s': Status %d, Body: %s", item.Path, fileResp.StatusCode, errorPayload))
				markdown.WriteString(fmt.Sprintf("Error: Proxy returned status %d for file '%s' (GitHub Raw URL: %s).\nResponse: %s\n\n", fileResp.StatusCode, item.Path, githubFileRawURL, errorPayload))
				continue
			}

			bodyBytes, readErr := io.ReadAll(fileResp.Body)
			fileResp.Body.Close()
			if readErr != nil {
				markdown.WriteString(fmt.Sprintf("Error reading proxied file content for '%s': %v\n\n", item.Path, readErr))
				continue
			}

			markdown.WriteString(fmt.Sprintf("```%s\n%s\n```\n\n", getFileExtension(item.Path), string(bodyBytes)))

		} else if item.Type == "dir" {
			// item.APIURL is the GitHub API URL for the directory's contents.
			// item.Path is the full path of the directory from the repo root.
			err := processDirectoryContents(item.APIURL, item.Path, markdown, depth+1, callback, maxRecursionDepth)
			if err != nil {
				js.Global().Get("console").Call("error", fmt.Sprintf("Error processing subdirectory '%s' (GitHub API URL: %s): %v", item.Path, item.APIURL, err))
				if strings.Contains(err.Error(), "rate limit") {
					return err // Propagate rate limit error up.
				}
			}
		} else if item.Type == "submodule" { // Handle submodule type
			js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Encountered submodule: %s", depth, item.Path))
			markdown.WriteString(fmt.Sprintf("### [SUBMODULE] %s\n(Content of submodule not transcribed)\n\n", item.Path))
		} else {
			js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Skipping item of unhandled type: %s, Type: %s", depth, item.Path, item.Type))
		}
	}
	js.Global().Get("console").Call("log", fmt.Sprintf("Exit processDirectoryContents: depth %d, URL: %s", depth, githubDirectoryAPIURL))
	return nil
}

func getFileExtension(filePath string) string {
	lastDot := strings.LastIndex(filePath, ".")
	if lastDot > -1 && lastDot < len(filePath)-1 {
		return strings.ToLower(filePath[lastDot+1:])
	}
	return ""
}

func isImageExtension(ext string) bool {
	imageExtensions := map[string]bool{"png": true, "jpg": true, "jpeg": true, "gif": true, "svg": true, "ico": true, "webp": true, "bmp": true}
	return imageExtensions[ext]
}

// isPotentiallyBinary checks for a high proportion of non-printable ASCII or null bytes.
// This is a simple heuristic.
func isPotentiallyBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	nonPrintable := 0
	limit := len(data)
	if limit > 512 { // Check only first 512 bytes for performance
		limit = 512
	}
	for i := 0; i < limit; i++ {
		b := data[i]
		if b == 0x00 { // Null byte is a strong indicator
			nonPrintable++
		} else if (b < 32 || b > 126) && b != '\n' && b != '\r' && b != '\t' {
			nonPrintable++
		}
	}
	// If more than 20% of checked bytes are non-printable/null, assume binary
	return float64(nonPrintable)/float64(limit) > 0.20
}

func isCommonTextFormat(ext string) bool {
	// Add extensions that are text-based but might trigger the binary heuristic
	// (e.g. minified JS, some data files)
	commonTextExts := map[string]bool{"json": true, "xml": true, "csv": true, "tsv": true, "md": true, "txt": true, "html": true, "css": true, "js": true, "ts": true, "py": true, "go": true, "java": true, "c": true, "cpp": true, "h": true, "hpp": true, "sh": true, "rb": true, "php": true, "yml": true, "yaml": true, "toml": true, "ini": true, "lock": true}
	return commonTextExts[ext]
}

func sendResult(callback js.Value, result Result) {
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		js.Global().Get("console").Call("error", "Failed to marshal result for callback:", err.Error())
		// Send a fallback error to JS if marshaling fails
		errorResult := `{"repo_name": "` + result.RepoName + `", "markdown": "Error: Could not serialize response from WASM."}`
		callback.Invoke(errorResult)
		return
	}
	callback.Invoke(string(jsonBytes))
}
