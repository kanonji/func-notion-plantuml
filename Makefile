build:
	mkdir -p functions
	cd ./src && go mod download && go build -o ../functions/v1 ./...