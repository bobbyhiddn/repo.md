import fs from 'fs-extra';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

function copyAssets(platform) {
  // Source and destination directories
  const srcDir = path.join(__dirname, '../src/assets');
  const distDir = path.join(__dirname, '../dist/assets');
  
  // Platform-specific directories
  const platformDirs = {
    ios: path.join(__dirname, '../ios/App/App/public/assets'),
    android: path.join(__dirname, '../android/app/src/main/assets/public/assets')
  };

  // Ensure directories exist
  fs.ensureDirSync(distDir);
  
  if (platform && platformDirs[platform]) {
    fs.ensureDirSync(platformDirs[platform]);
  }

  // Files to copy - main.wasm is our compiled Go code
  const filesToCopy = ['main.wasm', 'wasm_exec.js'];
  
  // Copy to dist
  filesToCopy.forEach(file => {
    const srcFile = path.join(srcDir, file);
    if (!fs.existsSync(srcFile)) {
      console.warn(`Warning: ${file} not found in src/assets`);
      return;
    }

    fs.copySync(
      srcFile,
      path.join(distDir, file),
      { overwrite: true }
    );
    console.log(`Copied ${file} to dist/assets`);

    // Copy to platform-specific directory if specified
    if (platform && platformDirs[platform]) {
      fs.copySync(
        srcFile,
        path.join(platformDirs[platform], file),
        { overwrite: true }
      );
      console.log(`Copied ${file} to ${platform} assets`);
    }
  });
}

try {
  // Get platform from command line argument
  const platform = process.argv[2];
  if (!platform) {
    console.log('No platform specified, copying to dist only');
  } else if (!['ios', 'android'].includes(platform)) {
    console.error('Invalid platform. Use "ios" or "android"');
    process.exit(1);
  }
  
  copyAssets(platform);
  console.log('Assets copied successfully');
} catch (err) {
  console.error('Error copying assets:', err);
  process.exit(1);
}
