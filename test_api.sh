#!/bin/bash

# Configuration
BASE_URL=${BASE_URL:-"http://localhost:8080/v1"}
API_KEY=${API_KEY:-"sk-router-admin-key"}

# Models to test based on the router configuration in main.go
# MODELS=("gpt-4" "claude" "gemini" "deepseek" "llama" "mistral" "tinydolphin")
MODELS=("tinydolphin:latest")

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Checking if server is running at ${BASE_URL}...${NC}"
if ! curl -s --connect-timeout 2 "${BASE_URL}/models" -H "Authorization: Bearer ${API_KEY}" > /dev/null; then
    echo -e "${RED}Error: Server is not responding at ${BASE_URL}${NC}"
    echo -e "Make sure to start the server first with: ${BLUE}go run cmd/server/main.go${NC}"
    exit 1
fi

echo -e "${YELLOW}Starting API Tests against ${BASE_URL}${NC}"
echo -e "${YELLOW}Using API Key: ${API_KEY}${NC}"

# Function to test listing models
test_list_models() {
    echo -e "\n${BLUE}Testing List Models${NC}"
    local response=$(curl -s -X GET "${BASE_URL}/models" -H "Authorization: Bearer ${API_KEY}")
    local count=$(echo "$response" | jq '.data | length')
    echo -e "${GREEN}Found ${count} models total.${NC}"
    echo -e "First 5 models:"
    echo "$response" | jq -r '.data[:5] | .[] | " - \(.id) (via \(.provider))"'
}

# Function to run non-streaming chat completion
test_non_streaming() {
    local model=$1
    echo -e "\n${BLUE}Testing Non-Streaming: model=${model}${NC}"    
    curl -s -X POST "${BASE_URL}/chat/completions" \
        -H "Authorization: Bearer ${API_KEY}" \
        -H "Content-Type: application/json" \
        -d "{
            \"model\": \"${model}\",
            \"messages\": [
                {\"role\": \"user\", \"content\": \"Hello, how are you? Respond in 10 words or less.\"}
            ],
            \"stream\": false
        }" | jq .
}

# Function to run streaming chat completion
test_streaming() {
    local model=$1
    echo -e "\n${BLUE}Testing Streaming: model=${model}${NC}"    
    curl -s -X POST "${BASE_URL}/chat/completions" \
        -H "Authorization: Bearer ${API_KEY}" \
        -H "Content-Type: application/json" \
        -d "{
            \"model\": \"${model}\",
            \"messages\": [
                {\"role\": \"user\", \"content\": \"Hello, how are you? Respond in 10 words or less.\"}
            ],
            \"stream\": true
        }"
}

# Run tests
# test_list_models

for model in "${MODELS[@]}"; do
    echo -e "\n${GREEN}=== Testing Model: ${model} ===${NC}"
    test_non_streaming "$model"
    test_streaming "$model"
    echo -e "\n${GREEN}=== Finished Testing Model: ${model} ===${NC}"
done

echo -e "\n${YELLOW}All tests completed.${NC}"
