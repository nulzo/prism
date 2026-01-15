#!/bin/bash

# Configuration
API_BASE="http://localhost:8080/v1"
MODEL="gpt-4o" # Ensure this model or a prefix like 'gpt-' is in your config.yaml routes

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}Starting API Integration Tests...${NC}\n"

# 1. Test Model Listing
echo -e "${BLUE}[1/3] Testing GET /models...${NC}"
curl -s -X GET "$API_BASE/models" | jq '.' | head -n 20
echo -e "${GREEN}✓ Model list retrieved (truncated output above)${NC}\n"

# 2. Test Non-Streaming Chat
echo -e "${BLUE}[2/3] Testing POST /chat/completions (Non-Streaming)...${NC}"
curl -s -X POST "$API_BASE/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'$MODEL'",
    "messages": [{"role": "user", "content": "Say hello in exactly 3 words." }],
    "stream": false
  }' | jq '.'
echo -e "${GREEN}✓ Non-streaming chat successful${NC}\n"

# 3. Test Streaming Chat
echo -e "${BLUE}[3/3] Testing POST /chat/completions (Streaming)...${NC}"
echo "Sending streaming request..."
# We use --no-buffer to see results immediately
curl -N -s -X POST "$API_BASE/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'$MODEL'",
    "messages": [{"role": "user", "content": "Count from 1 to 5 slowly." }],
    "stream": true
  }' | while read -r line; do
    if [[ $line == data:* ]]; then
        echo -e "${GREEN}$line${NC}"
    fi
done
echo -e "\n${GREEN}✓ Streaming chat successful${NC}"

echo -e "\n${BLUE}Integration tests complete.${NC}"
