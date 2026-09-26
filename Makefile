.PHONY: build run test tidy docker sessionhelper mcs-sim mcsd
build:
	./build.sh ./bin
run: build
	./bin/workpulse
test:
	./build.sh --test-only
	go test -count=1 ./...
tidy:
	go mod tidy
docker:
	@commit="$$(git rev-parse HEAD)"; \
	docker build --build-arg SOURCE_COMMIT="$$commit" -t workpulse:latest .
sessionhelper:
	python3 -m venv tools/sessionhelper/.venv
	tools/sessionhelper/.venv/bin/python -m pip install -r tools/sessionhelper/requirements.txt
	tools/sessionhelper/.venv/bin/python -m PyInstaller --clean tools/sessionhelper/sessionhelper.spec

# —— 多CLI调度模拟测试台(phase-2 E4):一条命令全跑,tester/后来者可独立复现 ——
# 剧本含真子进程 spawn,mcsd 包用 -p 1 串行;-count=3 验确定性。
mcs-sim:
	go vet ./internal/mcs/... ./cmd/mcsd
	CGO_ENABLED=0 go build -o bin/mcsd ./cmd/mcsd
	go test -race -count=1 ./internal/mcs/pool ./internal/mcs/board ./internal/mcs/dispatch
	go test -race -count=3 -p 1 ./internal/mcs/mcsd
	@echo "mcs-sim: ALL GREEN"

mcsd:
	CGO_ENABLED=0 go build -trimpath -o bin/mcsd ./cmd/mcsd

speclint:
	@scripts/speclint-gate.sh

speclint-hook:
	@ln -sf ../../scripts/speclint-gate.sh .git/hooks/pre-commit && echo "已装 pre-commit 钩子 -> scripts/speclint-gate.sh"
