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

// backendBaseURL will be set by JavaScript
var backendBaseURL string // This will store the URL passed from JS

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

// SetBackendBaseURL allows JavaScript to set the base URL for the backend API.
func SetBackendBaseURL(this js.Value, args []js.Value) interface{} {
	if len(args) > 0 && args[0].Type() == js.TypeString {
		backendBaseURL = args[0].String()
		js.Global().Get("console").Call("log", "Go WASM: Backend base URL set to:", backendBaseURL)
	} else {
		js.Global().Get("console").Call("error", "Go WASM: SetBackendBaseURL called with invalid arguments.")
	}
	return nil
}

// getActualAppBaseURL returns the configured backend base URL.
// It falls back to relative path if not set, but it's best to ensure it's always set via SetBackendBaseURL.
func getActualAppBaseURL() string {
	if backendBaseURL == "" {
		js.Global().Get("console").Call("warn", "Go WASM: backendBaseURL is not set by JavaScript. Falling back to relative path. This might not work as expected in all environments.")
		return "" // Fallback to relative path (e.g., for same-origin web deployment)
	}
	js.Global().Get("console").Call("log", "Go WASM: Using backend base URL:", backendBaseURL)
	return backendBaseURL
}

func main() {
	js.Global().Set("generateMarkdown", js.FuncOf(generateMarkdown))
	js.Global().Set("setBackendBaseURL", js.FuncOf(SetBackendBaseURL)) // Expose the new function
	<-make(chan bool)                                                  // Keep the WASM module alive
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
		// Construct the root API URL for the repository's contents
		// Default to the main branch if no specific branch/tag/commit is in the URL
		// Example: https://github.com/user/repo/tree/branch/path -> /repos/user/repo/contents/path?ref=branch
		// Example: https://github.com/user/repo -> /repos/user/repo/contents/
		var apiPath string
		var ref string

		// Check for /tree/branch or /blob/branch patterns
		// pathParts are relative to github.com (e.g. owner, repo, tree, branch, path...)
		urlPathParts := strings.Split(strings.TrimPrefix(originalURL, "https://github.com/"), "/")
		// urlPathParts[0] is owner, urlPathParts[1] is repo
		if len(urlPathParts) > 3 && (urlPathParts[2] == "tree" || urlPathParts[2] == "blob") {
			ref = urlPathParts[3]
			if len(urlPathParts) > 4 {
				apiPath = strings.Join(urlPathParts[4:], "/")
			}
		} else if len(urlPathParts) > 2 { // Path without explicit tree/blob, assume default branch
			apiPath = strings.Join(urlPathParts[2:], "/")
		}

		githubRootAPIURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, apiPath)
		if ref != "" {
			githubRootAPIURL = fmt.Sprintf("%s?ref=%s", githubRootAPIURL, ref)
		}
		// Trim trailing slash if apiPath was empty
		githubRootAPIURL = strings.TrimSuffix(githubRootAPIURL, "/")

		timestamp := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
		var markdownBuilder strings.Builder
		markdownBuilder.WriteString(fmt.Sprintf("# Repository: %s\nURL: %s\nTranscription Date: %s\n\n", repo, originalURL, timestamp))

		js.Global().Get("console").Call("log", "Starting processDirectoryContents via proxy for GitHub API URL: ", githubRootAPIURL)

		maxRecursionDepth := -1                               // Default to no limit, can be overridden by JS if needed
		if len(args) > 2 && args[2].Type() == js.TypeNumber { // Check if maxDepth is passed from JS
			maxRecursionDepth = int(args[2].Float())
			js.Global().Get("console").Call("log", "Go WASM: Max recursion depth set to:", maxRecursionDepth)
		}

		err := processDirectoryContents(githubRootAPIURL, "", &markdownBuilder, 0, callback, maxRecursionDepth)

		if err != nil {
			js.Global().Get("console").Call("error", "Error from processDirectoryContents: ", err.Error())
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
			// If context is done (e.g. browser tab closed), this might be the error.
			// js.Global().Get("console").Call("error", "Go WASM: client.Do error:", err.Error())
			return nil, err
		}

		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusForbidden { // 429 or 403 (often rate limit)
			return resp, nil
		}

		// Handle rate limiting (status 429 or 403 if X-RateLimit-Remaining is 0)
		isRateLimit := resp.StatusCode == http.StatusTooManyRequests
		if resp.StatusCode == http.StatusForbidden {
			if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining == "0" {
				isRateLimit = true
			}
		}

		if isRateLimit && i < maxRetries {
			resp.Body.Close()
			retryAfterHeader := resp.Header.Get("Retry-After")
			waitTimeSeconds := 60 // Default wait time
			if retryAfterHeader != "" {
				if s, errParse := time.ParseDuration(retryAfterHeader + "s"); errParse == nil {
					waitTimeSeconds = int(s.Seconds())
				}
			}
			// Apply exponential backoff with jitter
			backoff := math.Pow(2, float64(i))
			waitTime := time.Duration(int(backoff)*waitTimeSeconds) * time.Second / 2 // Add some jitter
			if waitTime < 1*time.Second {
				waitTime = 1 * time.Second
			} // Minimum 1s
			if waitTime > 300*time.Second {
				waitTime = 300 * time.Second
			} // Max 5 mins

			js.Global().Get("console").Call("log", fmt.Sprintf("Go WASM: Rate limited. Retrying in %v seconds...", waitTime.Seconds()))
			time.Sleep(waitTime)

			// Create a fresh request for retry as the body of the previous one might have been consumed or closed
			// For GET requests, the body is nil, so this is simpler.
			// If it were POST, we'd need to re-create the body.
			newReq, errClone := http.NewRequest(req.Method, req.URL.String(), nil)
			if errClone != nil {
				return nil, fmt.Errorf("failed to clone request for retry: %w", errClone)
			}
			// Copy headers
			for key, values := range req.Header {
				for _, value := range values {
					newReq.Header.Add(key, value)
				}
			}
			req = newReq // Use the new request for the next iteration
			continue
		} else if !isRateLimit { // Not a rate limit error, but not OK either, return immediately
			return resp, nil
		}
		// If it is a rate limit error but we've exhausted retries
		if isRateLimit && i >= maxRetries {
			js.Global().Get("console").Call("warn", "Go WASM: Rate limit retries exhausted.")
			return resp, nil // Return the last rate-limited response
		}
	}
	return resp, nil // Should be unreachable if loop logic is correct
}

func processDirectoryContents(githubDirectoryAPIURL string, currentItemPath string, markdown *strings.Builder, depth int, callback js.Value, maxRecursionDepth int) error {
	appBaseURL := getActualAppBaseURL() // Use the new getter
	proxyDirectoryURL := fmt.Sprintf("%s/api/proxy_github_api?url=%s", appBaseURL, url.QueryEscape(githubDirectoryAPIURL))

	displayPath := currentItemPath
	if displayPath == "" {
		displayPath = "repository root"
	}
	js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Proxying dir request for '%s' to: %s (Original GitHub API URL: %s)", depth, displayPath, proxyDirectoryURL, githubDirectoryAPIURL))

	if maxRecursionDepth >= 0 && depth > maxRecursionDepth { // Check if maxRecursionDepth is set (>=0)
		markdown.WriteString(fmt.Sprintf("\n*Skipping directory '%s' due to max recursion depth (%d).*\n", displayPath, maxRecursionDepth))
		return fmt.Errorf("max recursion depth %d reached for %s", maxRecursionDepth, displayPath)
	}

	req, err := http.NewRequest("GET", proxyDirectoryURL, nil)
	if err != nil {
		errMsg := fmt.Sprintf("\nError creating request for proxied directory '%s': %v\n", displayPath, err)
		markdown.WriteString(errMsg)
		return fmt.Errorf("http.NewRequest (proxy dir '%s'): %w", displayPath, err)
	}

	resp, err := makeRequestWithRetries(req, httpClient, 3)
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

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			marker := fmt.Sprintf("\n⚠️ GitHub API rate limit reached or authentication required for '%s'. Please try again later or provide a GitHub token.\nDetails: %s\n\n", displayPath, errorPayload)
			markdown.WriteString(marker)
			return fmt.Errorf("GitHub API rate limit or auth error for '%s': %d, %s", displayPath, resp.StatusCode, errorPayload)
		}

		marker := fmt.Sprintf("Error response from GitHub API for '%s'. Status: %d\nDetails: %s\n\n", displayPath, resp.StatusCode, errorPayload)
		markdown.WriteString(marker)
		return fmt.Errorf("GitHub API error response for '%s': %d, %s", displayPath, resp.StatusCode, errorPayload)
	}

	var items []GitHubItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		errMsg := fmt.Sprintf("\nError parsing proxied directory data for '%s' (from %s): %v\n", displayPath, proxyDirectoryURL, err)
		markdown.WriteString(errMsg)
		return fmt.Errorf("json.Decode (proxy dir '%s'): %w", displayPath, err)
	}

	if len(items) == 0 {
		if currentItemPath == "" {
			markdown.WriteString(fmt.Sprintf("\n*Repository '%s' appears to be empty or contains no processable items via proxy.*\n", displayPath))
		} else {
			markdown.WriteString(fmt.Sprintf("\n*Directory `%s` is empty or contains no processable items via proxy.*\n", currentItemPath))
		}
	}

	for _, item := range items {
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

			fileResp, fileErr := makeRequestWithRetries(fileReq, httpClient, 3)
			if fileErr != nil {
				markdown.WriteString(fmt.Sprintf("Error downloading proxied file '%s': %v\n\n", item.Path, fileErr))
				continue
			}
			defer fileResp.Body.Close()

			js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Proxy response for raw file '%s'. Status: %d", depth, item.Path, fileResp.StatusCode))

			if fileResp.StatusCode != http.StatusOK {
				fileBodyBytes, _ := io.ReadAll(fileResp.Body)
				// fileResp.Body.Close() // Already deferred
				errorPayload := string(fileBodyBytes)
				js.Global().Get("console").Call("error", fmt.Sprintf("Proxy error for raw file '%s': Status %d, Body: %s", item.Path, fileResp.StatusCode, errorPayload))
				markdown.WriteString(fmt.Sprintf("Error: Proxy returned status %d for file '%s' (GitHub Raw URL: %s).\nResponse: %s\n\n", fileResp.StatusCode, item.Path, githubFileRawURL, errorPayload))
				continue
			}

			bodyBytes, readErr := io.ReadAll(fileResp.Body)
			// fileResp.Body.Close() // Already deferred
			if readErr != nil {
				markdown.WriteString(fmt.Sprintf("Error reading proxied file content for '%s': %v\n\n", item.Path, readErr))
				continue
			}

			markdown.WriteString(fmt.Sprintf("```%s\n%s\n```\n\n", getFileExtension(item.Path), string(bodyBytes)))

		} else if item.Type == "dir" {
			err := processDirectoryContents(item.APIURL, item.Path, markdown, depth+1, callback, maxRecursionDepth)
			if err != nil {
				js.Global().Get("console").Call("error", fmt.Sprintf("Error processing subdirectory '%s' (GitHub API URL: %s): %v", item.Path, item.APIURL, err))
				if strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "auth error") {
					return err // Propagate critical errors up.
				}
			}
		} else if item.Type == "submodule" {
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
		ext := strings.ToLower(filePath[lastDot+1:])
		// Provide common language mappings for syntax highlighting
		switch ext {
		case "js":
			return "javascript"
		case "py":
			return "python"
		case "go":
			return "go"
		case "java":
			return "java"
		case "c", "h":
			return "c"
		case "cpp", "hpp", "cxx":
			return "cpp"
		case "cs":
			return "csharp"
		case "rb":
			return "ruby"
		case "php":
			return "php"
		case "swift":
			return "swift"
		case "kt", "kts":
			return "kotlin"
		case "rs":
			return "rust"
		case "ts":
			return "typescript"
		case "html", "htm":
			return "html"
		case "css":
			return "css"
		case "json":
			return "json"
		case "xml":
			return "xml"
		case "md":
			return "markdown"
		case "sh":
			return "shell"
		case "yaml", "yml":
			return "yaml"
		case "toml":
			return "toml"
		case "dockerfile", "Dockerfile":
			return "dockerfile"
		default:
			return ext
		}
	}
	return "" // No extension or unknown
}

// isImageExtension, isPotentiallyBinary, isCommonTextFormat are not used in this version
// as content is proxied and assumed to be text unless GitHub API indicates otherwise
// or if specific handling for binary types were to be re-added.

func sendResult(callback js.Value, result Result) {
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		js.Global().Get("console").Call("error", "Failed to marshal result for callback:", err.Error())
		errorResult := `{"repo_name": "` + result.RepoName + `", "markdown": "Error: Could not serialize response from WASM."}`
		callback.Invoke(errorResult)
		return
	}
	callback.Invoke(string(jsonBytes))
}
