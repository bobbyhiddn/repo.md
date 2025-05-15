from flask import Flask, request, jsonify
import os
import subprocess
import shutil
import tempfile
import logging
from datetime import datetime
from scribe_core import read_directory_to_markdown

# Configure basic logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
app = Flask(__name__, static_folder='capacitor/src', static_url_path='')

@app.route('/')
def index():
    return app.send_static_file('index.html')

@app.route('/generate_markdown', methods=['POST'])
def generate_markdown_route():
    data = request.get_json()
    github_url = data.get('github_url')
    if not github_url:
        return jsonify({"error": "GitHub URL is required."}), 400
    # Basic URL validation
    if not (github_url.startswith("https://github.com/") or github_url.startswith("git@github.com:")):
        if github_url.count('/') < 2:
            return jsonify({"error": "Invalid GitHub URL format."}), 400
    temp_dir = tempfile.mkdtemp(prefix="repo_scribe_")
    logging.info(f"Created temporary directory: {temp_dir}")
    try:
        logging.info(f"Cloning repository: {github_url} into {temp_dir}")
        clone = subprocess.run([
            'git', 'clone', '--depth', '1', github_url, temp_dir
        ], capture_output=True, text=True, timeout=180)
        if clone.returncode != 0:
            logging.error(f"Git clone failed: {clone.stderr}")
            return jsonify({"error": f"Git clone error: {clone.stderr.strip()}"}), 500
        repo_name = os.path.basename(github_url.rstrip('/'))
        if repo_name.endswith('.git'):
            repo_name = repo_name[:-4]
        timestamp = datetime.utcnow().strftime("%Y-%m-%d %H:%M:%S UTC")
        markdown = f"# Repository: {repo_name}\nURL: {github_url}\nTranscription Date: {timestamp}\n\n"
        markdown += read_directory_to_markdown(
            path=temp_dir,
            repo_root_path=temp_dir,
            current_markdown_path_prefix="",
            ignore_git=True,
            _initial_call=True,
            _gitignore_patterns_cache=None
        )
        return jsonify({"markdown": markdown, "repo_name": repo_name})
    except subprocess.TimeoutExpired:
        logging.error(f"Clone timed out for {github_url}")
        return jsonify({"error": "Cloning timed out."}), 500
    except Exception as e:
        logging.exception("Error generating markdown")
        return jsonify({"error": f"Unexpected error: {e}"}), 500
    finally:
        if os.path.exists(temp_dir):
            shutil.rmtree(temp_dir, ignore_errors=True)

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000, debug=True)
