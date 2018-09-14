# Custom Resource and Operator For MXNet jobs

This is a **fork** of the official kubeflow's tf-operator: [https://github.com/kubeflow/tf-operator](https://github.com/kubeflow/tf-operator). For more information about how the operator and the project is structured, refer to the unmodified master branch. 

In the default *mx-operator* branch you can find a rework of the project to support MXNet resources and distributed computation. The project structure is more or less the same as the original.

This project is considered as an exploratory work to understand the working of Kubernetes and custom operators, with the aim to build a more general Python operator for multiple deep learning frameworks: [https://github.com/StefanoFioravanzo/dl-operator](https://github.com/StefanoFioravanzo/dl-operator)

## Overview

The modifications to adapt the original tf-operator consisted in:

##### 1. Generation of clientsets

- Install needed dependencies: `k8s.io/gengo` and `github.com/goland/glog`
- Re-generate the proper client code for the new *mxjob* resource using the scripts in `vendor/k8s.io/code-generator/hack` (Get latest version: `go get k8s.io/code-generator`).

**NOTE**: Need also to run `go get k8s.io/apimachinery`, otherwise the code-gen will not work with the vendored version (see [https://github.com/kubernetes/code-generator/issues/21](https://github.com/kubernetes/code-generator/issues/21))

To generate the clientsets run in the folder `~/.go/src/k8s.io/code-generator`:

```bash
./generate-groups.sh defaulter github.com/kubeflow/tf-operator/pkg/client github.com/kubeflow/tf-operator/pkg/apis mxnet:v1alpha1 
```

##### 2. Recognize MXJob Resource

- Update the `types.go` for the mxnet jobs under `pkg/apis/mxnet/v1alpha1`
- Change all references in the code from TFJob to MXJob (to reflect the new `types.go` specification)
- Change the pod creation process updating the environment variables injected into the containers in `replicas.go`
- Various smaller changes here and there to make everything work.

--

For information about motivation and design for the
CRD please refer to
[tf_job_design_doc.md](tf_job_design_doc.md).

### Requirements

TFJob requires Kubernetes >= 1.8
 * CRDs required Kubernetes >= 1.7
 * TFJob depends on Garbage Collection for CRDs which is only supported
   in >= 1.8
 * GPU support is evolving quickly and its best to use Kubernetes 1.8
   to get the latest features.

## Creating a job

You create a job by defining a TFJob and then creating it with.

```
kubectl create -f examples/mxjob-linear-dist.yml
```

In this case the job spec looks like the following

```yaml
apiVersion: fioravanzo.org/v1alpha1
kind: MXJob
metadata:
  name: mx-test-dist
spec:
  replicaSpecs:
    - replicas: 1 # 1 scheduler
      mxReplicaType: SCHEDULER
      template:
        spec:
          containers:
            - image: stefanofioravanzo/mxnet-linear-dist:cpu
              name: mxnet
              imagePullPolicy: Always
          restartPolicy: OnFailure
    - replicas: 1 # 1 server
      mxReplicaType: SERVER
      template:
        spec:
          containers:
            - image: stefanofioravanzo/mxnet-linear-dist:cpu
              name: mxnet
              imagePullPolicy: Always
          restartPolicy: OnFailure
    - replicas: 1  # 1 worker
      mxReplicaType: WORKER
      template:
        spec:
          containers:
            - image: stefanofioravanzo/mxnet-linear-dist:cpu
              name: mxnet
              imagePullPolicy: Always
          restartPolicy: OnFailure
```

Each replicaSpec defines a set of MXNet processes.
The `mxReplicaType` defines the semantics for the set of processes.
The semantics are as follows

**scheduler**  

- A job must have 1 and only 1 scheduler
- The overall status of the MXJob is determined by the exit code of the
    mxnet container
	- 0 = success
	- 1-127 = permanent error
	- 128-255 = retryable error

**server**

- A job can have 0 to N servers
- Servers are automatically restarted if they exit

**workers**

- A job can have 0 to N workers
- Workers are automatically restarted if they exit

For each replica you define a **template** which is a K8s
[PodTemplateSpec](https://kubernetes.io/docs/api-reference/v1.8/#podtemplatespec-v1-core).
The template allows you to specify the containers, volumes, etc... that
should be created for each replica.

### Using GPUs

Using GPUs is as easy as adding the following spec in the workers' container spec:

```yaml
resources:
	limits:
		alpha.kubernetes.io/nvidia-gpu: 1
```

You also need to run code that supports GPU training. The job spec for GPU training would look like the following:

```yaml
apiVersion: fioravanzo.org/v1alpha1
kind: MXJob
metadata:
  name: mx-cifar-gpu
spec:
  replicaSpecs:
    - replicas: 1 # 1 Scheduler
      mxReplicaType: SCHEDULER
      template:
        spec:
          containers:
            - image: stefanofioravanzo/mxnet-cifar10-dist:gpu
              name: mxnet
              imagePullPolicy: Always
              volumeMounts: &volmount
          restartPolicy: OnFailure
          volumes: *cifarvol
    - replicas: 1 # 1 Server
      mxReplicaType: SERVER
      template:
        spec:
          containers:
            - image: stefanofioravanzo/mxnet-cifar10-dist:gpu
              name: mxnet
              imagePullPolicy: Always
              volumeMounts: &volmount
          restartPolicy: OnFailure
          volumes: *cifarvol
    - replicas: 1  # 1 Worker
      mxReplicaType: WORKER
      template:
        spec:
          containers:
            - image: stefanofioravanzo/mxnet-cifar10-dist:gpu
              name: mxnet
              imagePullPolicy: Always
              # Add GPU resource
              resources:
                limits:
                  alpha.kubernetes.io/nvidia-gpu: 1
              volumeMounts: &volmount
                - mountPath: /
                  name: myshare
          restartPolicy: OnFailure
          volumes: &cifarvol
           - name: myshare
             azureFile:
               secretName: mxsecret
               shareName: mxsn
               readOnly: false
```

In the previous job spec we are also mounting into the volumes a remote file share which stores the training data. For more information about how to achieve this refer to the repository: [https://github.com/StefanoFioravanzo/distributed-deeplearning-kubernetes](https://github.com/StefanoFioravanzo/distributed-deeplearning-kubernetes).