from flask import Flask, request, jsonify, Response
from flask_cors import CORS
import requests # For making HTTP requests from the backend
import os
# ... (keep other imports like subprocess, shutil, tempfile, logging, datetime, scribe_core if you still use parts of the old logic elsewhere,
# but for the proxy, 'requests' is key)

# Configure basic logging (if not already configured well)
import logging
logging.basicConfig(level=logging.DEBUG, format='%(asctime)s - %(levelname)s - %(message)s')
app = Flask(__name__, static_folder='static', static_url_path='')

# Enable CORS for all routes, allowing requests from the frontend
CORS(app, resources={
    r"/*": {
        "origins": ["http://localhost:3000", "http://127.0.0.1:3000"],
        "methods": ["GET", "POST", "OPTIONS"],
        "allow_headers": ["Content-Type"]
    }
})

# Enable debug mode for development
app.debug = True

# GitHub Token (Optional but recommended for higher rate limits)
# Store this as an environment variable in production
GITHUB_TOKEN = os.environ.get('GITHUB_API_TOKEN')

@app.route('/')
def index():
    # This should serve the HTML page that loads your WASM
    return app.send_static_file('index.html')

@app.route('/proxy_github_api', methods=['GET'])
def proxy_github_api():
    target_url = request.args.get('url')
    if not target_url or not target_url.startswith("https://api.github.com/"):
        logging.warning(f"Invalid or missing GitHub API URL received: {target_url}")
        return jsonify({"error": "Invalid or missing GitHub API URL for proxy."}), 400

    headers = {
        "Accept": "application/vnd.github.v3+json",
        "User-Agent": "RepoMD-Proxy/1.0 (+https://repo-md.com)"
    }
    if GITHUB_TOKEN:
        headers["Authorization"] = f"token {GITHUB_TOKEN}"
    else:
        logging.warning("GITHUB_TOKEN is not set. GitHub API requests will be unauthenticated, increasing rate limit risks and potential for non-JSON responses (e.g., CAPTCHAs).")

    try:
        logging.info(f"Proxying API request to: {target_url}")
        proxied_response = requests.get(target_url, headers=headers, timeout=30)
        
        logging.debug(f"GitHub raw response status: {proxied_response.status_code} for URL: {target_url}")
        logging.debug(f"GitHub raw response headers: {proxied_response.headers}")
        
        content_type = proxied_response.headers.get('Content-Type', '').lower()
        if proxied_response.status_code == 200 and 'application/json' not in content_type:
            logging.error(f"GitHub returned 200 OK for {target_url} but with unexpected Content-Type: {content_type}. Response text (first 500 chars): {proxied_response.text[:500]}")
            return jsonify({
                "error": "Proxy received non-JSON content type from GitHub despite 200 OK.",
                "github_status_code": proxied_response.status_code,
                "github_content_type": content_type,
                "details": "Expected JSON but received different content type. Check proxy logs."
            }), 502

        proxied_response.raise_for_status()
        
        response_data = proxied_response.json()
        return jsonify(response_data), proxied_response.status_code

    except requests.exceptions.HTTPError as e:
        logging.error(f"GitHub API HTTPError for {target_url}: {e.response.status_code} - Response text (first 500 chars): {e.response.text[:500]}")
        try:
            return jsonify(e.response.json()), e.response.status_code
        except (requests.exceptions.JSONDecodeError, ValueError):
            return jsonify({
                "error": "GitHub API error, non-JSON error response", 
                "status_code": e.response.status_code, 
                "details": e.response.text[:500]
            }), e.response.status_code

    except (requests.exceptions.JSONDecodeError, ValueError) as json_err:
        logging.error(f"JSONDecodeError for {target_url}. GitHub status: {proxied_response.status_code if 'proxied_response' in locals() else 'N/A'}. Error: {json_err}. Response text (first 500 chars): {proxied_response.text[:500] if 'proxied_response' in locals() else 'N/A'}")
        return jsonify({
            "error": "Proxy received non-JSON response or malformed JSON from GitHub API.",
            "github_status_code": proxied_response.status_code if 'proxied_response' in locals() else 'N/A',
            "details": "Expected JSON but received different content type or malformed JSON. Check proxy logs."
        }), 502

    except requests.exceptions.RequestException as e:
        logging.error(f"Network/RequestException proxying API request to {target_url}: {e}")
        return jsonify({"error": f"Failed to proxy request due to network/request issue: {str(e)}"}), 502

    except Exception as e:
        logging.error(f"Unexpected internal error in proxy_github_api for {target_url}: {e}", exc_info=True)
        return jsonify({"error": "An unexpected internal error occurred on the proxy server."}), 500


@app.route('/proxy_github_raw_content', methods=['GET'])
def proxy_github_raw_content():
    target_url = request.args.get('url')
    if not target_url or not target_url.startswith("https://raw.githubusercontent.com/"):
        return jsonify({"error": "Invalid or missing GitHub raw content URL for proxy."}), 400

    headers = {
        "User-Agent": "RepoMD-Proxy/1.0 (+https://repo-md.com)"
    }
    # No GitHub token needed usually for raw content, but can be added if private repo access was a feature
    # if GITHUB_TOKEN:
    #     headers["Authorization"] = f"token {GITHUB_TOKEN}"
    
    try:
        logging.info(f"Proxying raw content request to: {target_url}")
        # It's better to stream raw content
        proxied_response = requests.get(target_url, headers=headers, timeout=30, stream=True)
        proxied_response.raise_for_status()

        # Stream the response back to the client
        # Preserving the original content-type is important
        def generate():
            for chunk in proxied_response.iter_content(chunk_size=8192):
                yield chunk
        
        # Get content type from original response, default if not present
        content_type = proxied_response.headers.get('Content-Type', 'application/octet-stream')
        
        return Response(generate(), status=proxied_response.status_code, content_type=content_type)

    except requests.exceptions.HTTPError as e:
        logging.error(f"GitHub raw content HTTPError for {target_url}: {e.response.status_code} - {e.response.text}")
        return Response(e.response.text, status=e.response.status_code, mimetype='text/plain')
    except requests.exceptions.RequestException as e:
        logging.error(f"Error proxying raw content request to {target_url}: {e}")
        return jsonify({"error": f"Failed to proxy request: {e}"}), 502 # Bad Gateway
    except Exception as e:
        logging.error(f"Unexpected error in proxy_github_raw_content for {target_url}: {e}")
        return jsonify({"error": "An unexpected error occurred on the proxy."}), 500

# Keep your existing /generate_markdown route if it's still used for other purposes or as a fallback,
# otherwise, you might remove or refactor it if the WASM + proxy handles everything now.
# For example, the old server-side cloning route:
# @app.route('/generate_markdown', methods=['POST'])
# def generate_markdown_route():
#     # ... your existing server-side cloning logic ...
#     # This might now be deprecated by the WASM client-side approach.
#     pass


if __name__ == '__main__':
    # Make sure the static folder is correctly pointing to where your index.html and WASM assets are.
    # If your Dockerfile copies everything into /app/static, this should be fine.
    app.run(host='0.0.0.0', port=os.environ.get("PORT", 8080), debug=os.environ.get("FLASK_ENV") == "development")
