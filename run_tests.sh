#!/usr/bin/env bash

# usage:
# ./run_tests.sh                         # local, go 1.11
# GOVERSION=1.10 ./run_tests.sh          # local, go 1.10 (vgo)
# ./run_tests.sh docker                  # docker, go 1.11
# GOVERSION=1.10 ./run_tests.sh docker   # docker, go 1.10 (vgo)
# ./run_tests.sh podman                  # podman, go 1.11
# GOVERSION=1.10 ./run_tests.sh podman   # podman, go 1.10 (vgo)

set -ex

# The script does automatic checking on a Go package and its sub-packages,
# including:
# 1. gofmt         (http://golang.org/cmd/gofmt/)
# 2. go vet        (http://golang.org/cmd/vet)
# 3. gosimple      (https://github.com/dominikh/go-simple)
# 4. unconvert     (https://github.com/mdempsky/unconvert)
# 5. ineffassign   (https://github.com/gordonklaus/ineffassign)
# 6. race detector (http://blog.golang.org/race-detector)

# golangci-lint (github.com/golangci/golangci-lint) is used to run each each
# static checker.

# To run on docker on windows, symlink /mnt/c to /c and then execute the script
# from the repo path under /c.  See:
# https://github.com/Microsoft/BashOnWindows/issues/1854
# for more details.

# Default GOVERSION
[[ ! "$GOVERSION" ]] && GOVERSION=1.11
REPO=vhcseeder

testrepo () {
  GO=go
  if [[ $GOVERSION == 1.10 ]]; then
    GO=vgo
  fi

  $GO version

  # binary needed for RPC tests
  env CC=gcc $GO build
  cp "$REPO" "$GOPATH/bin/"

  # run tests on all modules
  ROOTPATH=$($GO list -m -f {{.Dir}} 2>/dev/null)
  ROOTPATHPATTERN=$(echo $ROOTPATH | sed 's/\\/\\\\/g' | sed 's/\//\\\//g')
  MODPATHS=$($GO list -m -f {{.Dir}} all 2>/dev/null | grep "^$ROOTPATHPATTERN"\
    | sed -e "s/^$ROOTPATHPATTERN//" -e 's/^\\\|\///')
  MODPATHS=". $MODPATHS"
  for module in $MODPATHS; do
    echo "==> ${module}"
    (cd $module && env GORACE='halt_on_error=1' CC=gcc $GO test -race \
	  ./...)
  done

  # check linters
  if [[ $GOVERSION != 1.10 ]]; then
    # linters do not work with modules yet
    golangci-lint run --disable-all --deadline=10m \
      --enable=gofmt \
      --enable=vet \
      --enable=gosimple \
      --enable=unconvert \
      --enable=ineffassign
    if [ $? != 0 ]; then
      echo 'golangci-lint has some complaints'
      exit 1
    fi
  fi

  echo "------------------------------------------"
  echo "Tests completed successfully!"
}

DOCKER=
[[ "$1" == "docker" || "$1" == "podman" ]] && DOCKER=$1
if [ ! "$DOCKER" ]; then
    testrepo
    exit
fi

# use Travis cache with docker
DOCKER_IMAGE_TAG=valhallacoin-golang-builder-$GOVERSION
$DOCKER pull valhallacoin/$DOCKER_IMAGE_TAG

$DOCKER run --rm -it -v $(pwd):/src:Z valhallacoin/$DOCKER_IMAGE_TAG /bin/bash -c "\
  rsync -ra --filter=':- .gitignore'  \
  /src/ /go/src/github.com/valhallacoin/$REPO/ && \
  cd github.com/valhallacoin/$REPO/ && \
  env GOVERSION=$GOVERSION GO111MODULE=on bash run_tests.sh"
