build:clean
	CGO_ENABLED=0 go build

TAG = latest
REPOSITORY = qingwave/watcher:${TAG}
dockerbuild:
	docker build -t ${REPOSITORY} .

dockerpush: dockerbuild
	docker push ${REPOSITORY}

clean:
	rm -f watcher