#!/bin/bash

PROJECT_ROOT="."
OPERATOR_SOURCE=${GOPATH}/src/github.com/kubeflow

rm -rf vendor/src
mkdir vendor/src
FILES=vendor/*
for f in $FILES
do
  if [ "$f" != "vendor/src" ]
  then
    FULL=`pwd`/$f
    echo "Symlinking vendor source dir: $FULL"
    ln -s "$FULL" vendor/src
  fi
done

# remove old symlink to this repo
rm -rf ${OPERATOR_SOURCE}/tf-operator
echo "Symlink project to GOPATH directory"
ln -s "${PROJECT_ROOT}" ${OPERATOR_SOURCE}
