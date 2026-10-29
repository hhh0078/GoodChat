#!/bin/sh
# 文件作用说明：用项目已有Go模块构建本机真实WS压测工具，不安装新依赖，不打印私有连接配置。
set -eu
task_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
task_build_dir="$task_root/.local/evidence/realtime-resilience/load"
mkdir -p "$task_build_dir"
cd "$task_root/backend"
go build -o "$task_build_dir/realtime-load" ../scripts/realtime-load/main.go ../scripts/realtime-load/fixture.go ../scripts/realtime-load/transport.go ../scripts/realtime-load/metrics.go ../scripts/realtime-load/runner.go ../scripts/realtime-load/cleanup.go
cd "$task_root"
exec "$task_build_dir/realtime-load" "$@"
