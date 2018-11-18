.PHONY: test image unit_test

IMAGE ?= diskfs/godiskfs:build

image:
	docker build -t $(IMAGE) testhelper/docker

dependencies:
	@dep ensure

unit_test: dependencies
	@go test ./...

test: image dependencies
	TEST_IMAGE=$(IMAGE) go test ./...
