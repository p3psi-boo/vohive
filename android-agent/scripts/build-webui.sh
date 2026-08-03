#!/bin/sh
# 构建 Agent 本地管理网页（PWA + SPA），产物输出到 app/src/main/assets/web/
set -e
cd "$(dirname "$0")/../webui"
npm ci
npm run build
