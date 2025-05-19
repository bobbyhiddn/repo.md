// capacitor/src/public/wasm-worker.js
console.log('WASM Worker: Script starting. Location:', self.location.href);

// Path to assets folder from wasm-worker.js (which will be at the root of dist/)
// wasm_exec.js and main.wasm are expected in dist/assets/
const ASSET_PATH = './assets/';

try {
    console.log('WASM Worker: Attempting to import wasm_exec.js from:', ASSET_PATH + 'wasm_exec.js');
    importScripts(ASSET_PATH + 'wasm_exec.js');
    console.log('WASM Worker: Successfully imported wasm_exec.js.');
} catch (e) {
    console.error("WASM Worker: Failed to import wasm_exec.js from:", ASSET_PATH + 'wasm_exec.js', e);
    self.postMessage({ type: 'wasmError', error: "Failed to load wasm_exec.js. Check console for path issues." });
    throw e; // Stop worker execution if critical script fails
}

let wasmReady = false;
let goInstance; // To store the Go instance, though not strictly needed by this JS if Go handles its own state

self.onmessage = async (event) => {
    const { type, url, data } = event.data; // 'url' can be GitHub repo URL or backend URL for setWasmBackendURL

    if (type === 'initWasm') {
        if (wasmReady) {
            self.postMessage({ type: 'wasmReady' });
            return;
        }
        if (typeof Go === 'undefined') {
            console.error("WASM Worker: Go global object not found. wasm_exec.js might not have loaded correctly.");
            self.postMessage({ type: 'wasmError', error: "Go global object not found. Ensure wasm_exec.js loaded." });
            return;
        }
        const go = new Go();
        goInstance = go; // Store the Go instance
        try {
            console.log('WASM Worker: Attempting to fetch main.wasm from:', ASSET_PATH + 'main.wasm');
            const result = await WebAssembly.instantiateStreaming(fetch(ASSET_PATH + 'main.wasm'), go.importObject);
            self.wasmInstance = result.instance; // Expose instance to self for debugging or direct calls if needed
            
            wasmReady = true; 
            self.postMessage({ type: 'wasmReady' }); // Notify main thread WASM module is loaded

            // Run the WASM instance. This call is blocking for the Go main() goroutine.
            // Go's main() should set up global functions (like setBackendBaseURL, generateMarkdown)
            // and then block (e.g., on <-make(chan bool)) to keep the module alive.
            await go.run(self.wasmInstance);
            console.log("WASM Worker: go.run finished. This means the Go program likely exited.");
            // If Go program exits, wasmReady might need to be reset or handled.
            // For a persistent Go service, main() should not exit.
        } catch (err) {
            console.error("WASM Worker: Failed to initialize WASM module", err);
            self.postMessage({ type: 'wasmError', error: `WASM Initialization Error: ${err.message || String(err)}` });
            wasmReady = false;
        }
    } else if (type === 'setWasmBackendURL') { 
        if (wasmReady && typeof self.setBackendBaseURL === 'function') {
            console.log("WASM Worker: Received setWasmBackendURL. Calling self.setBackendBaseURL with:", url);
            self.setBackendBaseURL(url); // 'url' is the backendBaseURL string
        } else {
            console.warn('WASM Worker: setBackendBaseURL message received, but WASM not ready or function not exposed.', 'WASM Ready:', wasmReady, 'setBackendBaseURL type:', typeof self.setBackendBaseURL);
            // Consider queuing this call if it's a timing issue, or signal error.
        }
    } else if (type === 'generateMarkdown') {
        if (!wasmReady || !self.wasmInstance || typeof self.generateMarkdown !== 'function') {
            console.error("WASM Worker: generateMarkdown called, but WASM not ready or function not exposed.", 'WASM Ready:', wasmReady, 'generateMarkdown type:', typeof self.generateMarkdown);
            self.postMessage({ type: 'error', message: 'WASM module is not ready or generateMarkdown function is unavailable.' });
            return;
        }
        const callback = (resultString) => { // This callback is invoked by Go
            self.postMessage({ type: 'markdownResult', result: resultString });
        };
        try {
            console.log("WASM Worker: Calling self.generateMarkdown with URL:", url, "and data:", data);
            // The Go function 'generateMarkdown' expects: url string, callback function, [optional maxDepth number]
            if (data && typeof data.maxDepth === 'number') {
                 self.generateMarkdown(url, callback, data.maxDepth);
            } else {
                 self.generateMarkdown(url, callback); // Call without maxDepth if not provided
            }
        } catch (err) {
            console.error("WASM Worker: Error occurred while calling self.generateMarkdown", err);
            self.postMessage({ type: 'error', error: `Error invoking Go's generateMarkdown: ${err.message || String(err)}` });
        }
    }
};
console.log("WASM Worker: Script loaded and event listener is set up.");
