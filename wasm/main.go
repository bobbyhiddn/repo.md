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

// GitHubItem represents an item in a GitHub repository (file or directory)
// We use `json:"url"` for APIURL because that's the field name from GitHub API for a directory's content listing URL.
// `DownloadURL` is specific to files for their raw content.

type GitHubItem struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	Type        string  `json:"type"`         // "file" or "dir"
	DownloadURL *string `json:"download_url"` // Pointer to handle null for directories
	APIURL      string  `json:"url"`          // API URL for this item's details or, if a dir, its contents
}

type Result struct {
	RepoName string `json:"repo_name"`
	Markdown string `json:"markdown"`
}

func main() {
	// Initialize HTTP client for WASM
	http.DefaultClient.Timeout = 30 * time.Second

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
			result = Result{Markdown: "Error: Invalid GitHub URL. Must contain 'github.com'"}
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
			// Append error to markdown, or handle more gracefully
			markdownBuilder.WriteString(fmt.Sprintf("\n\n--- ERROR DURING PROCESSING ---\n%v\n-----------------------------\n", err))
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

// processDirectoryContents recursively fetches and processes items in a directory.
func processDirectoryContents(directoryAPIURL string, markdown *strings.Builder, depth int, callback js.Value, maxRecursionDepth int) error {
	js.Global().Get("console").Call("log", fmt.Sprintf("Enter processDirectoryContents: depth %d, URL: %s", depth, directoryAPIURL))
	if depth > maxRecursionDepth {
		markdown.WriteString(fmt.Sprintf("\n*Skipping directory due to max recursion depth: %s*\n", directoryAPIURL))
		return fmt.Errorf("max recursion depth %d reached for %s", maxRecursionDepth, directoryAPIURL)
	}

	// Sleep before each new directory API call to be kind to the API and browser
	time.Sleep(100 * time.Millisecond)
	js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Attempting to GET directory: %s", depth, directoryAPIURL))
	resp, err := http.Get(directoryAPIURL)
	js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: GET directory response for %s. Status: %v, Error: %v", depth, directoryAPIURL, resp, err))
	if err != nil {
		markdown.WriteString(fmt.Sprintf("\nError fetching directory contents from %s: %v\n", directoryAPIURL, err))
		return fmt.Errorf("http.Get %s: %w", directoryAPIURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
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
		// Sleep for each item in the loop to give browser time
		time.Sleep(50 * time.Millisecond)

		if item.Type == "file" && item.DownloadURL != nil && *item.DownloadURL != "" {
			// Optional: progress update via callback
			// callback.Invoke(fmt.Sprintf(`{"status": "Fetching file: %s"}`, item.Path))
			markdown.WriteString(fmt.Sprintf("## %s\n\n", item.Path)) // Add file header
			js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: Attempting to GET file: %s, URL: %s", depth, item.Path, *item.DownloadURL))
			fileResp, fileErr := http.Get(*item.DownloadURL)
			js.Global().Get("console").Call("log", fmt.Sprintf("Depth %d: GET file response for %s. Status: %v, Error: %v", depth, item.Path, fileResp, fileErr))
			if fileErr != nil {
				markdown.WriteString(fmt.Sprintf("Error fetching file %s: %v\n\n", item.Path, fileErr))
				// fileResp might be nil here, so careful with fileResp.Body.Close()
				continue
			}

			if fileResp.StatusCode != http.StatusOK {
				markdown.WriteString(fmt.Sprintf("Error: GitHub API returned status %d for file %s\n\n", fileResp.StatusCode, item.Path))
				fileResp.Body.Close()
				continue
			}

			bodyBytes, readErr := io.ReadAll(fileResp.Body)
			fileResp.Body.Close() // Close body immediately

			if readErr != nil {
				markdown.WriteString(fmt.Sprintf("Error reading content of file %s: %v\n\n", item.Path, readErr))
				continue
			}
			ext := ""
			lastDot := strings.LastIndex(item.Path, ".")
			if lastDot > -1 && lastDot < len(item.Path)-1 {
				ext = strings.ToLower(item.Path[lastDot+1:])
			}
			if ext == "png" || ext == "jpg" || ext == "jpeg" || ext == "gif" || ext == "svg" || ext == "ico" || ext == "webp" {
				markdown.WriteString(fmt.Sprintf("### %s\n(Non-code file, content omitted)\n\n", item.Path))
			} else {
				markdown.WriteString(fmt.Sprintf("```%s\n%s\n```\n\n", ext, string(bodyBytes)))
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

func sendResult(callback js.Value, result Result) {
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		js.Global().Get("console").Call("error", "Failed to marshal result for callback:", err.Error())
		// Send a fallback error to JS if marshaling fails
		errorResult := `{"repo_name": "Internal Error", "markdown": "Error: Could not serialize response."}`
		callback.Invoke(errorResult)
		return
	}
	callback.Invoke(string(jsonBytes))
}
