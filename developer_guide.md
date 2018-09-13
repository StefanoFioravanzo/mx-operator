## Directories Layout

```sh
$ tree -d -I 'vendor|bin|.git'
.
├── build
│   ├── chart
│   │   └── mx-job-operator-chart
│   │       └── templates
│   │           └── tests
│   └── images
│       └── mx_operator
├── cmd
│   └── mx-operator
│       └── app
│           └── options
├── examples
├── hack
│   ├── boilerplate
│   └── scripts
├── pkg
│   ├── apis
│   │   └── mxnet
│   │       ├── helper
│   │       ├── v1alpha1
│   │       └── validation
│   ├── client
│   │   ├── clientset
│   │   │   └── versioned
│   │   │       ├── fake
│   │   │       ├── scheme
│   │   │       └── typed
│   │   │           └── mxnet
│   │   │               └── v1alpha1
│   │   │                   └── fake
│   │   ├── informers
│   │   │   └── externalversions
│   │   │       ├── internalinterfaces
│   │   │       └── mxnet
│   │   │           └── v1alpha1
│   │   └── listers
│   │       └── mxnet
│   │           └── v1alpha1
│   ├── controller
│   ├── trainer
│   └── util
│       └── k8sutil
└── version
```

## Building the Operator

Create a symbolic link inside your GOPATH to the location you checked out the code

```sh
mkdir -p ${GOPATH}/src/github.com/kubeflow
ln -sf ${GIT_TRAINING} ${GOPATH}/src/github.com/kubeflow/tf-operator
```

* GIT_TRAINING should be the location where you checked out https://github.com/kubeflow/tf-operator

Resolve dependencies (if you don't have glide install, check how to do it [here](https://github.com/Masterminds/glide/blob/master/README.md#install))

Install dependencies, `-v` will ignore subpackage vendor

```sh
glide install -v
```

Build it

```sh
go install github.com/kubeflow/tf-operator/cmd/mx-operator
```

## Building all the artifacts.

#### Docker image

You can run the Dockerfile under `build/images/mx_operator` from the root directory:

```
docker build -f ./build/images/mx_operator/Dockerfile -t <repository>/mx-operator .
```

#### Help chart for deployment

```bash
# initialization
helm init --upgrade
helm init

# create chart. Run command in build/chart
helm package  mx-job-operator-chart
```

## Running the Operator Locally

Running the operator locally (as opposed to deploying it on a K8s cluster) is convenient for debugging/development.

We can configure the operator to run locally using the configuration available in your kubeconfig to communicate with
a K8s cluster. Set your environment:

```sh
export KUBECONFIG=$(echo ~/.kube/config)
export KUBEFLOW_NAMESPACE=$(your_namespace)
```

* KUBEFLOW_NAMESPACE is used when deployed on Kubernetes, we use this variable to create other resources (e.g. the resource lock) internal in the same namespace. It is optional, use `default` namespace if not set.

Now we are ready to run operator locally:

```sh
kubectl create -f examples/crd.yaml
mx-operator --logtostderr
```

The first command creates a CRD `mxjobs`. And the second command runs the operator locally. To verify local
operator is working, create an example job and you should see jobs created by it.

```sh
kubectl create -f examples/mxjob-linear-dist.yml
```

## Go version

On ubuntu the default go package appears to be gccgo-go which has problems see [issue](https://github.com/golang/go/issues/15429) golang-go package is also really old so install from golang tarballs instead.

