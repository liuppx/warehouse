#!/bin/bash

BASE_URL="http://localhost:6065"
WALLET_ADDRESS="0x21b3eE0D9540D5fe07bCBeE7C056CA35FFFdcaEC"

echo "==================================="
echo "Testing Web3 Authentication"
echo "==================================="

# 1. 获取挑战
echo -e "\n1. POST Challenge"
CHALLENGE_RESPONSE=$(curl -s -X POST $BASE_URL/api/auth/challenge \
  -H "Content-Type: application/json" \
  -d "{\"address\": \"$WALLET_ADDRESS\"}")

echo "Challenge Response:"
echo $CHALLENGE_RESPONSE | jq .

CHALLENGE=$(echo $CHALLENGE_RESPONSE | jq -r .challenge)

echo -e "\nChallenge Message:"
echo "$CHALLENGE"

echo -e "\n==================================="
echo "Next Steps:"
echo "1. Copy the challenge message above"
echo "2. Sign it with your wallet (MetaMask, etc.)"
echo "3. Use the signature to verify and get token"
echo "==================================="

# 提示用户输入签名
echo -e "\nEnter the signature (or press Ctrl+C to exit):"
read SIGNATURE

if [ -n "$SIGNATURE" ]; then
  # 2. 验证签名并获取 token
  echo -e "\n2. Verify Signature and Get Token"
  TOKEN_RESPONSE=$(curl -s -X POST $BASE_URL/api/auth/verify \
    -H "Content-Type: application/json" \
    -d "{
      \"wallet_address\": \"$WALLET_ADDRESS\",
      \"signature\": \"$SIGNATURE\"
    }")
  
  echo "Token Response:"
  echo $TOKEN_RESPONSE | jq .
  
  TOKEN=$(echo $TOKEN_RESPONSE | jq -r .token)
  
  if [ "$TOKEN" != "null" ] && [ -n "$TOKEN" ]; then
    # 3. 使用 token 访问 WebDAV
    echo -e "\n3. Access WebDAV with Token"
    curl -s -X PROPFIND \
      -H "Authorization: Bearer $TOKEN" \
      -H "Depth: 1" \
      $BASE_URL/ | head -20
    
    echo -e "\n==================================="
    echo "Web3 Authentication Tests Complete"
    echo "Token: $TOKEN"
    echo "==================================="
  else
    echo "Failed to get token"
  fi
fi
