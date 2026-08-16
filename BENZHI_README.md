# BENZHI_README

## 项目说明
- 项目：benzhi-project-88b904e9-a667-4b5f-8966-dfe880e1e41a
- 项目用途：Implemented CoatWindow with the standard-library HTTP JSON workflow, atomic local ledger persistence, immutable close outcomes, defensive application collections, focused tests, README documentation, and terminating smoke validation. Both acceptance commands pass.
- Go 工具链：`golang:1.22.0`
- 前端工具链：无

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...
cd /app && GOTOOLCHAIN=local go run ./cmd/coatwindow -smoke
cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-project-88b904e9-a667-4b5f-8966-dfe880e1e41a-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-project-88b904e9-a667-4b5f-8966-dfe880e1e41a-arm64 linux/arm64
docker run -it benzhi-project-88b904e9-a667-4b5f-8966-dfe880e1e41a-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run ./cmd/coatwindow -smoke`
