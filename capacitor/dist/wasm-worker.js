// capacitor/src/public/wasm-worker.js
console.log('Worker starting, location:', self.location.href);

// Try different possible paths for the assets
let wasmExecFound = false;
let possiblePaths = [
  'assets/',
  '/assets/',
  '../assets/',
  './assets/',
  '../../assets/',
  '/'
];

for (const path of possiblePaths) {
  try {
    console.log('Trying to load wasm_exec.js from:', path + 'wasm_exec.js');
    self.importScripts(path + 'wasm_exec.js');
    console.log('Successfully loaded wasm_exec.js from:', path + 'wasm_exec.js');
    wasmExecFound = true;
    // Save the successful path for later use with main.wasm
    self.basePath = path;
    break;
  } catch (e) {
    console.error('Failed to load from path:', path, e);
  }
}

if (!wasmExecFound) {
  console.error('Failed to load wasm_exec.js from any path');
  self.postMessage({ type: 'wasmError', error: 'Could not load wasm_exec.js' });
}

let wasmReady = false;
let goInstance;

self.onmessage = async (event) => {
    const { type, url, data } = event.data;

    if (type === 'initWasm') {
        if (wasmReady) {
            self.postMessage({ type: 'wasmReady' });
            return;
        }
        const go = new Go();
        goInstance = go; // Store for potential future use if needed
        try {
            console.log('Attempting to fetch main.wasm from:', self.basePath + 'main.wasm');
            const result = await WebAssembly.instantiateStreaming(fetch(self.basePath + 'main.wasm'), go.importObject);
            console.log('Successfully loaded main.wasm');
            self.wasmInstance = result.instance; // 'self' makes it global-like within the worker
            wasmReady = true;
            self.postMessage({ type: 'wasmReady' });
            go.run(self.wasmInstance); // This promise never resolves, Go program keeps running
        } catch (err) {
            console.error("WASM Worker: Failed to initialize WASM", err);
            self.postMessage({ type: 'wasmError', error: err.message || String(err) });
            wasmReady = false;
        }
    } else if (type === 'generateMarkdown') {
        if (!wasmReady || !self.wasmInstance || typeof self.generateMarkdown !== 'function') {
            console.error("WASM Worker: WASM not ready or generateMarkdown function not found.");
            self.postMessage({ type: 'error', message: 'WASM module is not ready. Please wait or try refreshing.' });
            return;
        }

        const callback = (resultString) => {
            // Result from Go is already a stringified JSON
            self.postMessage({ type: 'markdownResult', result: resultString });
        };

        try {
            // generateMarkdown is set on js.Global() in Go, so it becomes `self.generateMarkdown` here
            self.generateMarkdown(url, callback);
        } catch (err) {
            console.error("WASM Worker: Error calling generateMarkdown", err);
            self.postMessage({ type: 'error', error: err.message || String(err) });
        }
    }
};
