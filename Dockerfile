FROM golang:1.21 AS wasm-builder

WORKDIR /go/src/repo.md

# Copy Go files
COPY wasm/ ./wasm/

# Build WASM module
RUN cd wasm && GOARCH=wasm GOOS=js go build -o main.wasm

# Copy wasm_exec.js
RUN cp /usr/local/go/misc/wasm/wasm_exec.js ./wasm/

# Final stage with Python
FROM python:3.11-slim

WORKDIR /app

# Install git for cloning repositories
RUN apt-get update && apt-get install -y git

# Install Python dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy the Flask application
COPY app.py scribe_core.py ./

# Create static directory structure
RUN mkdir -p static

# Copy frontend files directly from capacitor/src
COPY capacitor/src/ static/

# Copy WASM files
COPY --from=wasm-builder /go/src/repo.md/wasm/main.wasm static/assets/
COPY --from=wasm-builder /go/src/repo.md/wasm/wasm_exec.js static/assets/

EXPOSE 8080

CMD ["gunicorn", "--bind", "0.0.0.0:8080", "app:app"]
