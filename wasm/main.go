//go:build js && wasm
// +build js,wasm

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"syscall/js"
	"time"
)

// Define a package-level HTTP client
var httpClient = &http.Client{
	Timeout: 30 * time.Second, // Keep a reasonable timeout
}

const MAX_FILE_SIZE = 1 * 1024 * 1024 // 1MB limit for file content

// GitHubItem represents an item in a GitHub repository (file or directory)
// We use `json:"url"` for APIURL because that's the field name from GitHub API for a directory's content listing URL.
// `DownloadURL` is specific to files for their raw content.

type GitHubItem struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	Type        string  `json:"type"`         // "file" or "dir"
	DownloadURL *string `json:"download_url"` // Pointer to handle null for directories
	APIURL      string  `json:"url"`          // API URL for this item's details or, if a dir, its contents
	Size        int64   `json:"size"`         // File size in bytes
}

type Result struct {
	RepoName string `json:"repo_name"`
	Markdown string `json:"markdown"`
}

func main() {
	// Register the WASM function
	js.Global().Set("generateMarkdown", js.FuncOf(generateMarkdown))
	
	// Keep the program running
	<-make(chan bool)
}

func generateMarkdown(this js.Value, args []js.Value) interface{} {
	// Get callback function
	if len(args) < 2 {
		// Log error to JS console if callback is missing
		js.Global().Get("console").Call("error", "generateMarkdown called without a callback function")
		return nil
	}
	callback := args[1]

	go func() {
		var result Result

		// Validate URL
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

		// Extract owner/repo
		parts := strings.Split(strings.TrimSuffix(originalURL, "/"), "/")
		if len(parts) < 2 {
			result = Result{Markdown: "Error: Invalid GitHub URL format. Expected github.com/owner/repo"}
			sendResult(callback, result)
			return
		}
		owner := parts[len(parts)-2]
		repo := parts[len(parts)-1]
		rootAPIURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents", owner, repo)

		timestamp := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
		var markdownBuilder strings.Builder
		markdownBuilder.WriteString(fmt.Sprintf("# Repository: %s\nURL: %s\nTranscription Date: %s\n\n", repo, originalURL, timestamp))
		js.Global().Get("console").Call("log", "Starting processDirectoryContents from generateMarkdown for root: ", rootAPIURL)
		const maxRecursionDepth = 50 // Increased to handle deeper repository structures
		err := processDirectoryContents(rootAPIURL, &markdownBuilder, 0, callback, maxRecursionDepth)
		js.Global().Get("console").Call("log", "Finished processDirectoryContents from generateMarkdown for root. Error: ", err)
		if err != nil {
			// Append error to markdown in a user-friendly way
			if strings.Contains(err.Error(), "rate limit exceeded") {
				// We've already added detailed rate limit info in the markdown within processDirectoryContents
			} else {
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

// processDirectoryContents recursively fetches and processes items in a directory
func processDirectoryContents(directoryAPIURL string, markdown *strings.Builder, depth int, callback js.Value, maxRecursionDepth int) error {
	js.Global().Get("console").Call("log", fmt.Sprintf("Enter processDirectoryContents: depth %d, URL: %s", depth, directoryAPIURL))
	if depth > maxRecursionDepth {
		markdown.WriteString(fmt.Sprintf("\n*Skipping directory due to max recursion depth: %s*\n", directoryAPIURL))
		return fmt.Errorf("max recursion depth %d reached for %s", maxRecursionDepth, directoryAPIURL)
	}

	// No sleep needed - we're using proper request handling now
	js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Attempting to GET directory: %s", depth, directoryAPIURL))
	req, err := http.NewRequest("GET", directoryAPIURL, nil)
	if err != nil {
		markdown.WriteString(fmt.Sprintf("\nError creating request for %s: %v\n", directoryAPIURL, err))
		return fmt.Errorf("http.NewRequest %s: %w", directoryAPIURL, err)
	}
	// It's good practice to set a User-Agent
	req.Header.Set("User-Agent", "RepoMD-WASM-Client/1.0 (+https://repo-md.com)")
	req.Header.Set("Accept", "application/vnd.github.v3+json") // Be explicit

	resp, err := httpClient.Do(req)
	js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: API response for %s. Status: %v, Error: %v", depth, directoryAPIURL, resp, err))
	if err != nil {
		markdown.WriteString(fmt.Sprintf("\nError fetching directory contents from %s: %v\n", directoryAPIURL, err))
		return fmt.Errorf("http.Get %s: %w", directoryAPIURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Special handling for rate limit errors (common with GitHub API)
		if resp.StatusCode == http.StatusForbidden {
			markdown.WriteString("\n\n## GitHub API Rate Limit Exceeded\n\n")
			markdown.WriteString("The GitHub API rate limit has been reached. Please try again later or use a GitHub token for higher limits.\n\n")
			markdown.WriteString("* For unauthenticated requests, the rate limit allows for up to 60 requests per hour.\n")
			markdown.WriteString("* The rate limit resets hourly.\n\n")
			return fmt.Errorf("GitHub API rate limit exceeded")
		}

		// General error handling for other status codes
		markdown.WriteString(fmt.Sprintf("\nError: GitHub API returned status %d for directory %s\n", resp.StatusCode, directoryAPIURL))
		return fmt.Errorf("GitHub API status %d for %s", resp.StatusCode, directoryAPIURL)
	}

	var items []GitHubItem
	decodeErr := json.NewDecoder(resp.Body).Decode(&items)
	if decodeErr != nil {
		markdown.WriteString(fmt.Sprintf("\nError parsing directory data from %s: %v\n", directoryAPIURL, decodeErr))
		return fmt.Errorf("json.Decode %s: %w", directoryAPIURL, decodeErr)
	}

	if len(items) == 0 {
		markdown.WriteString(fmt.Sprintf("\n*Directory %s is empty or contains no processable items.*\n", directoryAPIURL))
		return nil
	}

	for _, item := range items {
		js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Processing item: Path: %s, Type: %s, API URL: %s, DownloadURL: %v", depth, item.Path, item.Type, item.APIURL, item.DownloadURL))

		if item.Type == "file" && item.DownloadURL != nil && *item.DownloadURL != "" {
			markdown.WriteString(fmt.Sprintf("## /%s\n", item.Path)) // Use item.Path as it's the full path

			if item.Size > MAX_FILE_SIZE {
				markdown.WriteString(fmt.Sprintf("\n[File too large (%d bytes, limit %d bytes), content omitted.]\n\n", item.Size, MAX_FILE_SIZE))
				continue
			}
            if item.Size == 0 { // Handle empty files
                markdown.WriteString(fmt.Sprintf("```%s\n[Empty File]\n```\n\n", getFileExtension(item.Path)))
                continue
            }

			fileReq, fileReqErr := http.NewRequest("GET", *item.DownloadURL, nil)
			if fileReqErr != nil {
				markdown.WriteString(fmt.Sprintf("Error creating request for file %s: %v\n\n", item.Path, fileReqErr))
				continue
			}
			fileReq.Header.Set("User-Agent", "RepoMD-WASM-Client/1.0 (+https://repo-md.com)")

			fileResp, fileErr := httpClient.Do(fileReq)
			if fileErr != nil {
				markdown.WriteString(fmt.Sprintf("Error fetching file %s: %v\n\n", item.Path, fileErr))
				continue
			}
			
			if fileResp.StatusCode != http.StatusOK {
				markdown.WriteString(fmt.Sprintf("Error: GitHub API returned status %d for file %s\n\n", fileResp.StatusCode, item.Path))
				fileResp.Body.Close()
				continue
			}

			bodyBytes, readErr := io.ReadAll(fileResp.Body)
			fileResp.Body.Close() 

			if readErr != nil {
				markdown.WriteString(fmt.Sprintf("Error reading content of file %s: %v\n\n", item.Path, readErr))
				continue
			}
			
			ext := getFileExtension(item.Path)
			if isImageExtension(ext) {
				markdown.WriteString(fmt.Sprintf("(Image file: `%s`, content omitted)\n\n", item.Name))
			} else {
				// Basic check for binary-like content (heuristic)
				if isPotentiallyBinary(bodyBytes) && !isCommonTextFormat(ext) {
						markdown.WriteString(fmt.Sprintf("(Potentially binary file: `%s`, content omitted)\n\n", item.Name))
				} else {
						markdown.WriteString(fmt.Sprintf("```%s\n%s\n```\n\n", ext, string(bodyBytes)))
				}
			}

		} else if item.Type == "dir" {
			js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Encountered directory: %s, API URL: %s", depth, item.Path, item.APIURL))
			// Optional: progress update via callback
			// callback.Invoke(fmt.Sprintf(`{"status": "Entering directory: %s"}`, item.Path))
			markdown.WriteString(fmt.Sprintf("### Directory: %s\n\n", item.Path)) // Add subdirectory header
			err := processDirectoryContents(item.APIURL, markdown, depth+1, callback, maxRecursionDepth)
			if err != nil {
				// Log or append error for this subdirectory, but continue with other items in parent dir
				markdown.WriteString(fmt.Sprintf("*Error processing subdirectory %s: %v*\n\n", item.Path, err))
			}
		} else if item.Type == "submodule" { // Handle submodule type
			js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Encountered submodule: %s", depth, item.Path))
			markdown.WriteString(fmt.Sprintf("### [SUBMODULE] %s\n(Content of submodule not transcribed)\n\n", item.Path))
		} else {
			js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Skipping item of unhandled type: %s, Type: %s", depth, item.Path, item.Type))
		}
	}
	js.Global().Get("console").Call("log", fmt.Sprintf("Exit processDirectoryContents: depth %d, URL: %s", depth, directoryAPIURL))
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
    commonTextExts := map[string]bool{"json": true, "xml": true, "csv": true, "tsv":true, "md": true, "txt": true, "html": true, "css": true, "js": true, "ts": true, "py": true, "go": true, "java": true, "c": true, "cpp": true, "h": true, "hpp": true, "sh": true, "rb": true, "php": true, "yml": true, "yaml": true, "toml": true, "ini": true, "lock": true}
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
