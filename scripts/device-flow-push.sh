#!/bin/bash
# GitHub 设备流轮询授权并推送（沙箱/CI 无交互 git 凭据时的推送通道）。
#
# 用法：先走 OAuth 设备流拿到 device_code（github.com/login/device 用
# CLIENT_ID 发起），再执行本脚本：
#   scripts/device-flow-push.sh <device_code>
# 成功后把 access_token 写进本仓库 remote 的 https URL 完成推送。
# CLIENT_ID 是本仓库发布方的 OAuth App 公开 ID（设备流不涉及 client_secret）。
CLIENT_ID="178c6fc778ccc68e1d6a"
DEVICE_CODE="$1"
if [ -z "$DEVICE_CODE" ]; then
  echo "USAGE: $0 <device_code>"
  exit 1
fi
INTERVAL=5
MAX=240
i=0
EMPTY_RETRIES=0

while [ "$i" -lt "$MAX" ]; do
  i=$((i + 1))
  RESP=$(curl -s -X POST https://github.com/login/oauth/access_token \
    -H "Accept: application/json" \
    -d "client_id=${CLIENT_ID}" \
    -d "device_code=${DEVICE_CODE}" \
    -d "grant_type=urn:ietf:params:oauth:grant-type:device_code" \
    --connect-timeout 10 --max-time 45)
  # 空响应/非 JSON：网络瞬态故障，继续轮询（上限 60 次）
  if [ -z "$RESP" ] || ! echo "$RESP" | grep -q '"'; then
    EMPTY_RETRIES=$((EMPTY_RETRIES + 1))
    if [ "$EMPTY_RETRIES" -gt 60 ]; then
      echo "TOO_MANY_EMPTY_RESPONSES"
      exit 5
    fi
    sleep "$INTERVAL"
    continue
  fi
  ERR=$(echo "$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin).get('error',''))" 2>/dev/null)
  if [ "$ERR" = "authorization_pending" ]; then
    sleep "$INTERVAL"
    continue
  fi
  if [ "$ERR" = "slow_down" ]; then
    INTERVAL=$((INTERVAL + 5))
    sleep "$INTERVAL"
    continue
  fi
  if [ "$ERR" = "expired_token" ]; then
    echo "DEVICE_FLOW_EXPIRED"
    exit 2
  fi
  TOKEN=$(echo "$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin).get('access_token',''))" 2>/dev/null)
  if [ -n "$TOKEN" ]; then
    echo "DEVICE_FLOW_OK"
    cd /root/.codebuddy/artifact/gox || exit 1
    git remote set-url origin "https://oauth2:${TOKEN}@github.com/RobertPanVIP/gox.git"
    git push origin main 2>&1
    exit $?
  fi
  echo "UNKNOWN_ERROR: $RESP"
  exit 3
done
echo "DEVICE_FLOW_TIMEOUT"
exit 4
