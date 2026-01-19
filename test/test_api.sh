#!/bin/bash

# Configuration
API_HOST="http://localhost:8080"
API_BASE="${API_HOST}/server/v1"
MODEL="${MODEL:-ollama/tinydolphin:latest}"
TEMP_BODY="response_body.json" # Temp file for JSON body

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

# Counters
TESTS_PASSED=0
TESTS_FAILED=0

# Clean up temp file on exit
trap "rm -f $TEMP_BODY" EXIT

# --- Helper Functions ---

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_pass() { echo -e "${GREEN}[PASS]${NC} $1"; ((TESTS_PASSED++)); }
log_fail() { echo -e "${RED}[FAIL]${NC} $1"; ((TESTS_FAILED++)); }

assert_status() {
    local code=$1
    local expected=$2
    local ctx=$3
    if [ "$code" -eq "$expected" ]; then
        log_pass "Status $expected for $ctx"
        return 0
    else
        log_fail "Status for $ctx. Expected $expected, got $code"
        return 1
    fi
}

assert_json_field() {
    local field=$1
    local expected=$2
    local ctx=$3
    # Read from the temp file
    local actual=$(jq -r "$field" < "$TEMP_BODY") 
    
    if [ "$actual" == "$expected" ]; then
        log_pass "$ctx: $field is '$expected'"
    else
        log_fail "$ctx: Expected $field to be '$expected', got '$actual'"
        return 1
    fi
}

# --- Main Execution ---

# 1. Health
log_info "Testing Health..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$API_HOST/health")
assert_status "$HTTP_CODE" 200 "GET /health"

# 2. List Models
log_info "Testing List Models..."
# Write body to file, capture code in var
HTTP_CODE=$(curl -s -o "$TEMP_BODY" -w "%{http_code}" "$API_BASE/models")

if assert_status "$HTTP_CODE" 200 "GET /models"; then
    assert_json_field ".object" "list" "List Models"
    
    COUNT=$(jq '.data | length' < "$TEMP_BODY")
    if [ "$COUNT" -gt 0 ]; then
        log_pass "List Models: Found $COUNT models"
    else
        log_fail "List Models: No models found"
    fi
fi

# 3. Chat Completion (Non-Streaming)
log_info "Testing Chat Completion (Sync)..."
PAYLOAD=$(jq -n --arg model "$MODEL" '{ 
    model: $model,
    messages: [{role: "user", content: "Say hello"}],
    stream: false
}')

# Write body to TEMP_BODY, capture status code
HTTP_CODE=$(curl -s -o "$TEMP_BODY" -w "%{http_code}" -X POST "$API_BASE/chat/completions" \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD")

if assert_status "$HTTP_CODE" 200 "POST /chat/completions (Sync)"; then
    assert_json_field ".object" "chat.completion" "Chat Sync"
    
    # Check if content exists and is not null/empty
    CONTENT=$(jq -r ".choices[0].message.content // empty" < "$TEMP_BODY")
    if [ -n "$CONTENT" ] && [ "$CONTENT" != "null" ]; then
         log_pass "Chat Sync: Content received"
    else
         log_fail "Chat Sync: Content is empty"
    fi
fi

# 4. Chat Completion (Streaming)
log_info "Testing Chat Completion (Streaming)..."
PAYLOAD=$(jq -n --arg model "$MODEL" '{ 
    model: $model,
    messages: [{role: "user", content: "Count to 3"}],
    stream: true
}')

# Use a separate file for streaming response to avoid conflict
STREAM_FILE=$(mktemp)
curl -N -s -X POST "$API_BASE/chat/completions" \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD" > "$STREAM_FILE" &
PID=$!

# Wait briefly for response
sleep 2
kill $PID 2>/dev/null

if grep -q "data:" "$STREAM_FILE"; then
    log_pass "Chat Streaming: Received SSE data chunks"
else
    log_fail "Chat Streaming: No SSE data received"
    # Optional: cat "$STREAM_FILE" to see what happened
fi
rm "$STREAM_FILE"

# 5. Error Handling (Bad Request)
log_info "Testing Validation Error..."
PAYLOAD='{
    "messages": [{"role": "user", "content": "Fail me"}]
}'

# Using TEMP_BODY here too for consistency
HTTP_CODE=$(curl -s -o "$TEMP_BODY" -w "%{http_code}" -X POST "$API_BASE/chat/completions" \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD")

# Expect 400
if [[ "$HTTP_CODE" == "400" ]]; then
    log_pass "Validation Error: Received expected error code 400"
    
    # Optional: Verify it returns your RFC 9457 shape
    assert_json_field ".title" "Validation Error" "Error Response"
else
    log_fail "Validation Error: Expected 400, got $HTTP_CODE"
fi

echo "----------------------------------------"
echo -e "Tests Completed: ${GREEN}$TESTS_PASSED Passed${NC}, ${RED}$TESTS_FAILED Failed${NC}"

if [ "$TESTS_FAILED" -eq 0 ]; then
    exit 0
else
    exit 1
fi