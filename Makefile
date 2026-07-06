.PHONY: build run test tidy docker sessionhelper
build:
	CGO_ENABLED=0 go build -trimpath -o bin/workpulse ./cmd/workpulse
run: build
	./bin/workpulse
test:
	go test ./...
tidy:
	go mod tidy
docker:
	docker build -t workpulse:latest .
sessionhelper:
	python3 -m venv tools/sessionhelper/.venv
	tools/sessionhelper/.venv/bin/python -m pip install -r tools/sessionhelper/requirements.txt
	tools/sessionhelper/.venv/bin/python -m PyInstaller --clean tools/sessionhelper/sessionhelper.spec
