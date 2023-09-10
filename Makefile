hornet-storage: $(shell find .. -name "*.go")
	CC=$$(which musl-gcc) go build -ldflags='-s -w -linkmode external -extldflags "-static"' -o ./hornet-storage ./services/server/main.go