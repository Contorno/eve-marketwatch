DEFAULT_TAGET: buildDocker

fmt:
	go fmt ./...
.PHONY:fmt

lint: fmt
	golint ./...
.PHONY:lint

vet: fmt
	go vet ./...
	# shadow ./...
.PHONY:vet

buildApp: vet
	CGO_ENABLED=0 GOOS=linux go build -a -o bin/eve-marketwatch ./cmd/
.PHONY:buildApp

buildDocker: buildApp
	docker build -t contorno/eve-marketwatch .
	docker tag contorno/eve-marketwatch:latest 357769355421.dkr.ecr.us-west-2.amazonaws.com/eve-marketwatch:latest
.PHONY:buildDocker

uploadDocker: buildDocker
	aws ecr get-login-password --region us-west-2 | docker login --username AWS --password-stdin 357769355421.dkr.ecr.us-west-2.amazonaws.com
	docker push 357769355421.dkr.ecr.us-west-2.amazonaws.com/eve-marketwatch:latest
.PHONY:uploadDocker