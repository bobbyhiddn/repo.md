import fs from 'fs-extra';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

function copyAssets(platform) {
  // Source and destination directories
  const srcDir = path.join(__dirname, '../src/public/assets'); // Vite will copy from src/public/assets to dist/assets
  const distDir = path.join(__dirname, '../dist/assets');
  
  // Platform-specific directories
  const platformDirs = {
    ios: path.join(__dirname, '../ios/App/App/public/assets'),
    android: path.join(__dirname, '../android/app/src/main/assets/public/assets')
  };

  // Ensure platform-specific directories exist if a platform is specified
  if (platform && platformDirs[platform]) {
    fs.ensureDirSync(platformDirs[platform]);
  } else if (platform) {
    console.warn(`Warning: Platform ${platform} specified but no directory mapping found.`);
    return; // Don't proceed if platform is specified but unknown
  }

  // If no platform is specified, this script will now do nothing further,
  // as Vite handles copying from src/public/assets to dist/assets.

  // Files to copy - WASM files
  const filesToCopy = ['main.wasm', 'wasm_exec.js'];
  
  // Copy to dist
  filesToCopy.forEach(file => {
    const srcFile = path.join(srcDir, file);
    if (!fs.existsSync(srcFile)) {
      console.warn(`Warning: ${file} not found in src/assets`);
      return;
    }

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
  if (platform && !['ios', 'android'].includes(platform)) {
    console.error('Invalid platform. Use "ios" or "android"');
    process.exit(1);
  }
  
  if (platform) {
    copyAssets(platform);
    console.log(`Assets copied successfully for ${platform}`);
  } else {
    console.log('No platform specified. Vite will handle copying assets to dist. This script will not copy to platform-specific directories.');
  }
} catch (err) {
  console.error('Error copying assets:', err);
  process.exit(1);
}
