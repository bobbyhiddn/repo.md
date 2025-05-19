from flask import Flask, request, jsonify, Response
from flask_cors import CORS
import requests
import os
import logging

logging.basicConfig(level=logging.DEBUG, format='%(asctime)s - %(levelname)s - %(message)s')
app = Flask(__name__, static_folder='static', static_url_path='')

# Determine if running in production (Fly.io sets the PORT environment variable)
is_production = "PORT" in os.environ

# Define allowed origins
dev_origins = [
    "http://localhost:3000", 
    "http://127.0.0.1:3000", 
    "http://localhost:5173", # Vite dev server
    "http://127.0.0.1:5173"  # Vite dev server
]
prod_origins = [
    "https://repo-md.com",    # Your main production domain
    "capacitor://localhost",  # Default Capacitor iOS/Android origin
    "ionic://localhost"       # Often used by Capacitor/Ionic apps
]

current_origins = prod_origins if is_production else dev_origins

if is_production:
    logging.info(f"Production CORS enabled for: {current_origins}")
else:
    logging.info(f"Development CORS enabled for: {current_origins}")

CORS(app, resources={
    r"/api/*": { # Apply CORS only to /api/ routes
        "origins": current_origins,
        "methods": ["GET", "POST", "OPTIONS"], # Ensure OPTIONS is included for preflight requests
        "allow_headers": ["Content-Type", "Authorization"], # Add Authorization if you plan to use it
        "supports_credentials": True # If you ever use cookies or auth headers that need this
    }
})

# Enable debug mode for development (Fly.io will likely override this in prod)
app.debug = not is_production

# GitHub Token (Optional but recommended for higher rate limits)
# Store this as an environment variable in production
GITHUB_TOKEN = os.environ.get('GITHUB_API_TOKEN')

@app.route('/')
def index():
    # This should serve the HTML page that loads your WASM
    return app.send_static_file('index.html')

@app.route('/api/proxy_github_api', methods=['GET'])
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
        logging.warning("GITHUB_TOKEN is not set. GitHub API requests will be unauthenticated, increasing rate limit risks.")

    try:
        logging.info(f"Proxying API request to: {target_url}")
        proxied_response = requests.get(target_url, headers=headers, timeout=30)
        
        logging.debug(f"GitHub raw response status: {proxied_response.status_code} for URL: {target_url}")
        
        # Forward relevant headers from GitHub's response
        response_headers = {
            "Content-Type": proxied_response.headers.get("Content-Type", "application/json"),
        }
        if "Link" in proxied_response.headers:
            response_headers["Link"] = proxied_response.headers["Link"]
        if "X-RateLimit-Limit" in proxied_response.headers:
            response_headers["X-RateLimit-Limit"] = proxied_response.headers["X-RateLimit-Limit"]
        if "X-RateLimit-Remaining" in proxied_response.headers:
            response_headers["X-RateLimit-Remaining"] = proxied_response.headers["X-RateLimit-Remaining"]
        if "X-RateLimit-Reset" in proxied_response.headers:
            response_headers["X-RateLimit-Reset"] = proxied_response.headers["X-RateLimit-Reset"]

        # Check for non-JSON content type if status is 200 OK
        content_type_header = proxied_response.headers.get('Content-Type', '').lower()
        if proxied_response.status_code == 200 and 'application/json' not in content_type_header:
            logging.error(f"GitHub returned 200 OK for {target_url} but with unexpected Content-Type: {content_type_header}. Response text (first 500 chars): {proxied_response.text[:500]}")
            # Still return the content, but log it as an issue. The client might handle non-JSON.
            # Or, decide to return an error to the client:
            # return jsonify({
            #     "error": "Proxy received non-JSON content type from GitHub despite 200 OK.",
            #     "github_status_code": proxied_response.status_code,
            #     "github_content_type": content_type_header
            # }), 502
        
        # For non-200 responses, try to return GitHub's error payload if it's JSON
        if proxied_response.status_code != 200:
            try:
                error_data = proxied_response.json()
                return jsonify(error_data), proxied_response.status_code, response_headers
            except requests.exceptions.JSONDecodeError:
                # If GitHub's error isn't JSON, return its text content
                return Response(proxied_response.text, status=proxied_response.status_code, headers=response_headers, mimetype=content_type_header)

        return Response(proxied_response.content, status=proxied_response.status_code, headers=response_headers)

    except requests.exceptions.RequestException as e:
        logging.error(f"Network/RequestException proxying API request to {target_url}: {e}")
        return jsonify({"error": f"Failed to proxy request due to network/request issue: {str(e)}"}), 502
    except Exception as e:
        logging.error(f"Unexpected internal error in proxy_github_api for {target_url}: {e}", exc_info=True)
        return jsonify({"error": "An unexpected internal error occurred on the proxy server."}), 500


@app.route('/api/proxy_github_raw_content', methods=['GET'])
def proxy_github_raw_content():
    target_url = request.args.get('url')
    if not target_url or not target_url.startswith("https://raw.githubusercontent.com/"):
        return jsonify({"error": "Invalid or missing GitHub raw content URL for proxy."}), 400

    headers = {
        "User-Agent": "RepoMD-Proxy/1.0 (+https://repo-md.com)"
    }
    
    try:
        logging.info(f"Proxying raw content request to: {target_url}")
        proxied_response = requests.get(target_url, headers=headers, timeout=30, stream=True)
        proxied_response.raise_for_status() # Raise HTTPError for bad responses (4xx or 5xx)

        def generate():
            for chunk in proxied_response.iter_content(chunk_size=8192):
                yield chunk
        
        content_type = proxied_response.headers.get('Content-Type', 'application/octet-stream')
        
        # Forward relevant headers
        response_headers = {
            "Content-Type": content_type,
        }
        # Add other headers like Content-Disposition if present and relevant

        return Response(generate(), status=proxied_response.status_code, headers=response_headers)

    except requests.exceptions.HTTPError as e:
        logging.error(f"GitHub raw content HTTPError for {target_url}: {e.response.status_code} - {e.response.text}")
        # Return GitHub's error response directly
        return Response(e.response.text, status=e.response.status_code, mimetype=e.response.headers.get('Content-Type', 'text/plain'))
    except requests.exceptions.RequestException as e:
        logging.error(f"Error proxying raw content request to {target_url}: {e}")
        return jsonify({"error": f"Failed to proxy request: {e}"}), 502 
    except Exception as e:
        logging.error(f"Unexpected error in proxy_github_raw_content for {target_url}: {e}", exc_info=True)
        return jsonify({"error": "An unexpected error occurred on the proxy."}), 500

if __name__ == '__main__':
    port = int(os.environ.get("PORT", 8080)) # Fly.io sets PORT, Gunicorn uses this. For local dev, can be 8081 if preferred.
    app.run(host='0.0.0.0', port=port, debug=not is_production)
