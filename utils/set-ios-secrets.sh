#!/bin/bash

# Load environment variables from .env file
if [ -f .env ]; then
    export $(cat .env | grep -v '^#' | xargs)
else
    echo "Error: .env file not found"
    echo "Please copy .env.example to .env and fill in your values"
    exit 1
fi

# Function to format API key content
format_api_key() {
    local key_file="$1"
    # Remove header and footer lines, join all lines, and remove any existing newlines
    sed -n '/-BEGIN PRIVATE KEY-/,/-END PRIVATE KEY-/p' "$key_file" | \
    grep -v "PRIVATE KEY" | \
    tr -d '\n'
}

# App Store Connect API Keys
gh secret set APPLE_KEY_ID --body "$APPLE_KEY_ID"
gh secret set APPLE_ISSUER_ID --body "$APPLE_ISSUER_ID"

# Format and set API key content from .p8 file
if [ -f ".github/workflows/auth/AuthKey_$APPLE_KEY_ID.p8" ]; then
    KEY_CONTENT=$(format_api_key ".github/workflows/auth/AuthKey_$APPLE_KEY_ID.p8")
    gh secret set APPLE_KEY_CONTENT --body "$KEY_CONTENT"
else
    echo "Error: API key file not found at .github/workflows/auth/AuthKey_$APPLE_KEY_ID.p8"
    exit 1
fi

# App and Developer Information
gh secret set DEVELOPER_APP_ID --body "$DEVELOPER_APP_ID"
gh secret set DEVELOPER_APP_IDENTIFIER --body "$DEVELOPER_APP_IDENTIFIER"
gh secret set DEVELOPER_PORTAL_TEAM_ID --body "$DEVELOPER_PORTAL_TEAM_ID"
gh secret set FASTLANE_APPLE_ID --body "$FASTLANE_APPLE_ID"
gh secret set FASTLANE_USER --body "$FASTLANE_USER"
gh secret set FASTLANE_PASSWORD --body "$FASTLANE_PASSWORD"

# Match Configuration
gh secret set MATCH_PASSWORD --body "$MATCH_PASSWORD"
gh secret set PROVISIONING_PROFILE_SPECIFIER --body "$PROVISIONING_PROFILE_SPECIFIER"

# Additional Required Secrets
gh secret set APP_STORE_CONNECT_TEAM_ID --body "$APP_STORE_CONNECT_TEAM_ID"
gh secret set GIT_URL --body "$GIT_URL"
gh secret set GIT_AUTHORIZATION --body "$GIT_AUTHORIZATION"
gh secret set TEMP_KEYCHAIN_PASSWORD --body "$TEMP_KEYCHAIN_PASSWORD"

# Construct and set MATCH_GIT_URL
MATCH_GIT_URL="https://${GIT_AUTHORIZATION}@github.com/bobbyhiddn/fastlane.git"
gh secret set MATCH_GIT_URL --body "$MATCH_GIT_URL"

echo "iOS secrets have been set successfully!"
echo ""
echo "⚠️ Note: Make sure your .env file contains all required values"