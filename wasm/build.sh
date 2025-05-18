#!/bin/bash

# Build the WASM binary
GOOS=js GOARCH=wasm go build -o ../capacitor/src/public/assets/main.wasm

# Copy wasm_exec.js to assets
cp "$(go env GOROOT)/misc/wasm/wasm_exec.js" ../capacitor/src/public/assets/

# Also copy to web directory for direct web use
cp ../capacitor/src/public/assets/main.wasm ../wasm/
cp ../capacitor/src/public/assets/wasm_exec.js ../wasm/
