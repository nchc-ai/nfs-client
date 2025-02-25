COMMIT_HASH = $(shell git rev-parse --short HEAD)
BRANCH_NAME = $(shell git rev-parse --abbrev-ref HEAD)
RET = $(shell git describe --contains $(COMMIT_HASH) 1>&2 2> /dev/null; echo $$?)
USER = $(shell whoami)
IS_DIRTY_CONDITION = $(shell git diff-index --name-status HEAD | wc -l)

REPO = ghcr.io/nchc-ai
IMAGE = $(notdir $(CURDIR))

ifeq ($(strip $(IS_DIRTY_CONDITION)), 0)
	# if clean,  IS_DIRTY tag is not required
	IS_DIRTY = $(shell echo "")
else
	# add dirty tag if repo is modified
	IS_DIRTY = $(shell echo "-dirty")
endif

# Image Tag rule
# 1. Use git tag if we found
# 2. if repo in a master branch, use <username>-<commit-hash>
# 3. if repo in non-master branch, use branch name as image tag
ifeq ($(RET),0)
	TAG = $(shell git describe --contains $(COMMIT_HASH))$(IS_DIRTY)
else
	ifeq ($(BRANCH_NAME), main)
		TAG = $(USER)-$(COMMIT_HASH)$(IS_DIRTY)
	else
		TAG = $(BRANCH_NAME)$(IS_DIRTY)
	endif
endif

build:
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o docker/x86_64/nfs-client-provisioner ./cmd/nfs-client-provisioner

image:
	docker build -t $(REPO)/$(IMAGE):$(TAG) -f docker/build-in-docker/Dockerfile .



