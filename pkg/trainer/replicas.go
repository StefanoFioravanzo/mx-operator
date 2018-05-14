// Copyright 2018 The Kubeflow Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package trainer

import (
	//"encoding/json"
	"errors"
	"fmt"
	"strings"

	log "github.com/golang/glog"
	"k8s.io/api/core/v1"
	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sErrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	mxv1alpha1 "github.com/kubeflow/tf-operator/pkg/apis/mxnet/v1alpha1"
	"github.com/kubeflow/tf-operator/pkg/util/k8sutil"
	// TOOO(jlewi): Rename to apiErrors
	"github.com/kubeflow/tf-operator/pkg/apis/mxnet/helper"
	"github.com/kubeflow/tf-operator/pkg/util"

)

const (
	SuccessfulCreateReason = "SuccessfulCreate"
	FailedCreateReason     = "FailedCreate"
)

// MXReplicaSet is a set of MX processes all acting as the same role (e.g. worker)
type MXReplicaSet struct {
	ClientSet kubernetes.Interface
	recorder  record.EventRecorder
	// Job is a pointer to the TrainingJob to which this replica belongs.
	Job  *TrainingJob
	Spec mxv1alpha1.MXReplicaSpec
}

// MXReplicas is an interface for managing a set of replicas.
type MXReplicaSetInterface interface {
	Create() error
	Delete() error
	GetStatus() (mxv1alpha1.MXReplicaStatus, error)
}

// TFConfig is a struct representing the TensorFlow config. This struct is turned into an environment
// which is used by TensorFlow processes to configure themselves.
type TFConfig struct {
	// Cluster represents a TensorFlow ClusterSpec.
	// See: https://www.tensorflow.org/api_docs/python/tf/train/ClusterSpechttps://www.tensorflow.org/api_docs/python/tf/train/ClusterSpec
	Cluster ClusterSpec `json:"cluster"`
	Task    TaskSpec    `json:"task"`
	// Environment is used by tensorflow.contrib.learn.python.learn in versions <= 1.3
	// TODO(jlewi): I don't think it is used in versions TF >- 1.4. So we can eventually get rid of it.
	Environment string `json:"environment"`
}

type MXConfig struct {
	Role string			//"DMLC_ROLE": "scheduler",
	PSUri string		//"DMLC_PS_ROOT_URI": "127.0.0.1",
	PSPort string		//"DMLC_PS_ROOT_PORT": "9000",
	NumServer int32		//"DMLC_NUM_SERVER": "1",
	NumWorker int32		//"DMLC_NUM_WORKER": "1",
	Verbosity int32		//"PS_VERBOSE": "2"
}

func NewTFReplicaSet(clientSet kubernetes.Interface, recorder record.EventRecorder, mxReplicaSpec mxv1alpha1.MXReplicaSpec, job *TrainingJob) (*MXReplicaSet, error) {
	defer Exit(Enter("replicas.go $FN"))
	if mxReplicaSpec.MXReplicaType == mxv1alpha1.SCHEDULER && *mxReplicaSpec.Replicas != 1 {
		// TODO(stefano): Could generate the schedule by default without having to declare it on the yml file
		return nil, errors.New("The SCHEDULER must have Replicas = 1")
	}

	if mxReplicaSpec.MXPort == nil {
		return nil, errors.New("mxReplicaSpec.TFPort can't be nil.")
	}

	if mxReplicaSpec.Template == nil && mxReplicaSpec.MXReplicaType != mxv1alpha1.SERVER {
		return nil, fmt.Errorf("tfReplicamxv1alpha1.Template can't be nil for replica type %v.", mxReplicaSpec.MXReplicaType)
	}

	// Make sure the replica type is valid.
	validReplicaTypes := []mxv1alpha1.MXReplicaType{mxv1alpha1.SCHEDULER, mxv1alpha1.SERVER, mxv1alpha1.WORKER}

	isValidReplicaType := false
	for _, t := range validReplicaTypes {
		if t == mxReplicaSpec.MXReplicaType {
			isValidReplicaType = true
			break
		}
	}

	if !isValidReplicaType {
		return nil, fmt.Errorf("mxReplicaSpec.MXReplicaType is %v but must be one of %v", mxReplicaSpec.MXReplicaType, validReplicaTypes)
	}

	return &MXReplicaSet{
		ClientSet: clientSet,
		recorder:  recorder,
		Job:       job,
		Spec:      mxReplicaSpec,
	}, nil
}

// Labels returns the labels for this replica set.
func (s *MXReplicaSet) Labels() KubernetesLabels {
	defer Exit(Enter("replicas.go $FN"))
	return KubernetesLabels(map[string]string{
		"kubeflow.org": "",
		"job_type":     string(s.Spec.MXReplicaType),
		// runtime_id is set by Job.setup, which is called after the MXReplicaSet is created.
		// this is why labels aren't a member variable.
		"runtime_id":  s.Job.job.Spec.RuntimeId,
		"tf_job_name": s.Job.job.ObjectMeta.Name})
}

// CreateServiceWithIndex will create a new service with specify index
func (s *MXReplicaSet) CreateServiceWithIndex(index int32) (*v1.Service, error) {
	defer Exit(Enter("replicas.go $FN"))
	taskLabels := s.Labels()
	taskLabels["task_index"] = fmt.Sprintf("%v", index)

	// Create the service.
	service := &v1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:   s.genName(index),
			Labels: taskLabels,
			OwnerReferences: []meta_v1.OwnerReference{
				helper.AsOwner(s.Job.job),
			},
		},
		Spec: v1.ServiceSpec{
			Selector: taskLabels,
			Ports: []v1.ServicePort{
				{
					Name: "mx-port",
					Port: *s.Spec.MXPort,
				},
			},
		},
	}

	log.Infof("Creating service: %v", service.ObjectMeta.Name)
	return s.ClientSet.CoreV1().Services(s.Job.job.ObjectMeta.Namespace).Create(service)
}

// CreatePodWithIndex will create a new pod with specify index
func (s *MXReplicaSet) CreatePodWithIndex(index int32) (*v1.Pod, error) {
	defer Exit(Enter("replicas.go $FN"))
	taskLabels := s.Labels()
	taskLabels["task_index"] = fmt.Sprintf("%v", index)

	pod := &v1.Pod{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:   s.genPodName(index),
			Labels: taskLabels,
			OwnerReferences: []meta_v1.OwnerReference{
				helper.AsOwner(s.Job.job),
			},
		},
		Spec: *s.Spec.Template.Spec.DeepCopy(),
	}

	pod.Spec.SchedulerName = s.Job.SchedulerName()


	//tfConfig := TFConfig{
	//	Cluster: s.Job.ClusterSpec(),
	//	Task: TaskSpec{
	//		Type:  strings.ToLower(string(s.Spec.MXReplicaType)),
	//		Index: int(index),
	//	},
	//	// We need to set environment to cloud  otherwise it will default to local which isn't what we want.
	//	Environment: "cloud",
	//}
	//
	//tfConfigJson, err := json.Marshal(tfConfig)
	//if err != nil {
	//	log.Errorf("Job: %v serializing tfConfig: %v return error; %v", s.Job.job.ObjectMeta.Name, util.Pformat(tfConfig), err)
	//	return nil, err
	//}

	// Configure the MXNET environment variables for distributed training
	for i, _ := range pod.Spec.Containers {
		// We can't get c in the loop variable because that would be by value so our modifications
		// wouldn't have any effect.
		c := &pod.Spec.Containers[i]
		if c.Name != mxv1alpha1.DefaultMXContainer {
			continue
		}
		if len(c.Env) == 0 {
			c.Env = make([]v1.EnvVar, 0)
		}
		println("DMLC_ROLE: " + s.Spec.MXReplicaType)
		println("DMLC_PS_ROOT_PORT : " + string(*s.Spec.MXPort))
		println("DMLC_PS_ROOT_URI: " + s.Job.Replicas[0].genName(0))


		// get the number of replicas for this node type
		//s.Job.Replicas[0].Spec.Replicas  // to access to the replica number of the yml job file
		var (
			numWorkers int32 = 0
			numServers int32 = 0
			schedulerHostname string = ""
		)
		for i, _ := range s.Job.Replicas {
			c := s.Job.Replicas[i]
			if c.Spec.MXReplicaType == mxv1alpha1.WORKER {
				numWorkers = *c.Spec.Replicas
			}
			if c.Spec.MXReplicaType == mxv1alpha1.SERVER {
				numServers = *c.Spec.Replicas
			}
			// TODO(stefano): add support for multiple schedulers. Need to find a way to call genName with different indexes.
			// Maybe populate this value before this and embed it into the Replica structure
			if c.Spec.MXReplicaType == mxv1alpha1.SCHEDULER {
				schedulerHostname = c.genName(0)
			}
		}

		c.Env = append(c.Env, v1.EnvVar{
			Name:  "DMLC_ROLE",
			Value: strings.ToLower(string(s.Spec.MXReplicaType)),
		})
		c.Env = append(c.Env, v1.EnvVar{
			Name:  "DMLC_PS_ROOT_URI",
			//Value: s.Job.Replicas[0].genName(0),
			Value: schedulerHostname,
		})
		c.Env = append(c.Env, v1.EnvVar{
			Name:  "DMLC_PS_ROOT_PORT",
			// TODO(stefano): Change the definition of this port to the SCHEDULER's port
			Value: fmt.Sprint(*s.Spec.MXPort),
		})
		c.Env = append(c.Env, v1.EnvVar{
			Name:  "DMLC_NUM_SERVER",
			Value: fmt.Sprint(numServers),
		})
		c.Env = append(c.Env, v1.EnvVar{
			Name:  "DMLC_NUM_WORKER",
			Value: fmt.Sprint(numWorkers),
		})
		c.Env = append(c.Env, v1.EnvVar{
			Name:  "PS_VERBOSE",
			Value: "2",
		})

		//if s.Spec.MXReplicaType == mxv1alpha1.SCHEDULER || s.Spec.MXReplicaType == mxv1alpha1.SERVER {
		//	c.Command = append(c.Command, "python3")
		//	c.Command = append(c.Command, "-c")
		//	c.Command = append(c.Command, "'import mxnet'")
		//} else if s.Spec.MXReplicaType == mxv1alpha1.WORKER {
		//	// Use command provided by Dockerfile
		//	// TODO (stefano): think about how to handle this
		//	//c.Command = append(c.Command, "python3 /app/main.py")
		//}
	}

	log.Infof("Creating pod: %v", pod.ObjectMeta.Name)
	return s.ClientSet.CoreV1().Pods(s.Job.job.ObjectMeta.Namespace).Create(pod)
}

// Delete deletes the replicas
func (s *MXReplicaSet) Delete() error {
	defer Exit(Enter("replicas.go $FN"))
	selector, err := s.Labels().ToSelector()
	if err != nil {
		return err
	}

	failures := false

	options := meta_v1.ListOptions{
		LabelSelector: selector,
	}

	log.V(1).Infof("Deleting Jobs namespace=%v selector=%v", s.Job.job.ObjectMeta.Namespace, selector)
	err = s.ClientSet.CoreV1().Pods(s.Job.job.ObjectMeta.Namespace).DeleteCollection(&meta_v1.DeleteOptions{}, options)

	if err != nil {
		log.Errorf("There was a problem deleting the jobs; %v", err)
		failures = true
	}

	// We need to delete the completed pods.
	log.Infof("Deleting Pods namespace=%v selector=%v", s.Job.job.ObjectMeta.Namespace, selector)
	err = s.ClientSet.CoreV1().Pods(s.Job.job.ObjectMeta.Namespace).DeleteCollection(&meta_v1.DeleteOptions{}, options)

	if err != nil {
		log.Errorf("There was a problem deleting the pods; %v", err)
		failures = true
	}

	// Services doesn't support DeleteCollection so we delete them individually.
	// TODO(jlewi): We should check if this has changed with K8s 1.8 or other releases.
	for index := int32(0); index < *s.Spec.Replicas; index++ {
		log.V(1).Infof("Deleting Service %v:%v", s.Job.job.ObjectMeta.Namespace, s.genName((index)))
		err = s.ClientSet.CoreV1().Services(s.Job.job.ObjectMeta.Namespace).Delete(s.genName(index), &meta_v1.DeleteOptions{})

		if err != nil {
			log.Errorf("Error deleting service %v; %v", s.genName(index), err)
			failures = true
		}
	}

	// If the ConfigMap for the default parameter server exists, we delete it
	log.Infof("Get ConfigMaps %v:%v", s.Job.job.ObjectMeta.Namespace, s.defaultPSConfigMapName())
	_, err = s.ClientSet.CoreV1().ConfigMaps(s.Job.job.ObjectMeta.Namespace).Get(s.defaultPSConfigMapName(), meta_v1.GetOptions{})
	if err != nil {
		if !k8sutil.IsKubernetesResourceNotFoundError(err) {
			log.Errorf("Error deleting ConfigMap %v; %v", s.defaultPSConfigMapName(), err)
			failures = true
		}
	} else {
		log.Infof("Delete ConfigMaps %v:%v", s.Job.job.ObjectMeta.Namespace, s.defaultPSConfigMapName())
		err = s.ClientSet.CoreV1().ConfigMaps(s.Job.job.ObjectMeta.Namespace).Delete(s.defaultPSConfigMapName(), &meta_v1.DeleteOptions{})
		if err != nil {
			log.Errorf("There was a problem deleting the ConfigMaps; %v", err)
			failures = true
		}
	}

	if failures {
		return errors.New("Some of the replicas resources could not be deleted")
	}
	return nil
}

// replicaStatusFromPodList returns a status from a list of pods for a job.
func replicaStatusFromPodList(l v1.PodList, name string) mxv1alpha1.ReplicaState {
	defer Exit(Enter("replicas.go $FN"))
	var latest *v1.Pod
	for _, i := range l.Items {
		if latest == nil {
			latest = &i
			continue
		}
		if latest.Status.StartTime.Before(i.Status.StartTime) {
			latest = &i
		}
	}

	if latest == nil {
		return mxv1alpha1.ReplicaStateRunning
	}

	var tfState v1.ContainerState

	for _, i := range latest.Status.ContainerStatuses {
		if i.Name != name {
			continue
		}

		// We need to decide whether to use the current state or the previous termination state.
		tfState = i.State

		// If the container previously terminated we will look at the termination to decide whether it is a retryable
		// or permanenent error.
		if i.LastTerminationState.Terminated != nil {
			tfState = i.LastTerminationState
		}
	}

	if tfState.Running != nil || tfState.Waiting != nil {
		return mxv1alpha1.ReplicaStateRunning
	}

	if tfState.Terminated != nil {
		if tfState.Terminated.ExitCode == 0 {
			return mxv1alpha1.ReplicaStateSucceeded
		}

		if isRetryableTerminationState(tfState.Terminated) {
			// Since its a retryable error just return RUNNING.
			// We can just let Kubernetes restart the container to retry.
			return mxv1alpha1.ReplicaStateRunning
		}

		return mxv1alpha1.ReplicaStateFailed
	}

	return mxv1alpha1.ReplicaStateUnknown
}

func (s *MXReplicaSet) GetSingleReplicaStatus(index int32) mxv1alpha1.ReplicaState {
	defer Exit(Enter("replicas.go $FN"))
	p, err := s.ClientSet.CoreV1().Pods(s.Job.job.ObjectMeta.Namespace).Get(s.genName(index), meta_v1.GetOptions{})

	if err != nil {
		return mxv1alpha1.ReplicaStateUnknown
	}

	if v1.PodSucceeded == p.Status.Phase {
		return mxv1alpha1.ReplicaStateSucceeded
	}

	labels := s.Labels()
	labels["task_index"] = fmt.Sprintf("%v", index)
	selector, err := labels.ToSelector()
	if err != nil {
		log.Errorf("labels.ToSelector() error; %v", err)
		return mxv1alpha1.ReplicaStateFailed
	}

	// TODO(jlewi): Handle errors. We need to get the pod and looking at recent container exits.
	l, err := s.ClientSet.CoreV1().Pods(s.Job.job.ObjectMeta.Namespace).List(meta_v1.ListOptions{
		// TODO(jlewi): Why isn't the label selector working?
		LabelSelector: selector,
	})

	if err != nil {
		// TODO(jlewi): Are there errors that should be treated as retryable errors?
		return mxv1alpha1.ReplicaStateFailed
	}

	status := replicaStatusFromPodList(*l, mxv1alpha1.DefaultMXContainer)
	return status
}

// Status returns the status of the replica set.
func (s *MXReplicaSet) GetStatus() (mxv1alpha1.MXReplicaStatus, error) {
	defer Exit(Enter("replicas.go $FN"))
	status := mxv1alpha1.MXReplicaStatus{
		MXReplicaType:  s.Spec.MXReplicaType,
		State:          mxv1alpha1.ReplicaStateUnknown,
		ReplicasStates: make(map[mxv1alpha1.ReplicaState]int),
	}

	increment := func(state mxv1alpha1.ReplicaState) {
		v, ok := status.ReplicasStates[state]
		if ok {
			status.ReplicasStates[state] = v + 1
		} else {
			status.ReplicasStates[state] = 1
		}
	}

	for index := int32(0); index < *s.Spec.Replicas; index++ {
		increment(s.GetSingleReplicaStatus(index))
	}

	// Determine the overall status for the replica set based on the status of the individual
	// replicas.
	// If any of the replicas failed mark the set as failed.
	if _, ok := status.ReplicasStates[mxv1alpha1.ReplicaStateFailed]; ok {
		status.State = mxv1alpha1.ReplicaStateFailed
		return status, nil
	}

	// If any replicas are RUNNING mark it as RUNNING.
	if _, ok := status.ReplicasStates[mxv1alpha1.ReplicaStateRunning]; ok {
		status.State = mxv1alpha1.ReplicaStateRunning
		return status, nil
	}

	// If all of the replicas succeeded consider it success.
	if v, ok := status.ReplicasStates[mxv1alpha1.ReplicaStateSucceeded]; ok && int32(v) == *s.Spec.Replicas {
		status.State = mxv1alpha1.ReplicaStateSucceeded
		return status, nil
	}

	return status, nil
}

// SyncPods will try to check current pods for this TFReplicaSet and try to make it as desired.
func (s *MXReplicaSet) SyncPods() error {
	defer Exit(Enter("replicas.go $FN"))
	for index := int32(0); index < *s.Spec.Replicas; index++ {

		// Label to get all pods of this TFReplicaType + index
		labels := s.Labels()
		labels["task_index"] = fmt.Sprintf("%v", index)

		labelSelector, err := labels.ToSelector()
		if err != nil {
			return err
		}

		// Filter the unactive pods
		fieldSelector := "status.phase!=" + string(v1.PodFailed) +
			",deletionTimestamp!=nil"

		options := meta_v1.ListOptions{
			LabelSelector: labelSelector,
			FieldSelector: fieldSelector,
		}

		// List to get pods
		pl, err := s.ClientSet.CoreV1().Pods(s.Job.job.ObjectMeta.Namespace).List(options)

		if len(pl.Items) == 0 {
			log.Infof("Pod  not found, create new one.")
			// Create the pod
			createdPod, err := s.CreatePodWithIndex(index)

			// If the pod already exists do nothing.
			if err != nil {
				if k8s_errors.IsAlreadyExists(err) {
					log.Infof("Pod: %v already exists.", createdPod.ObjectMeta.Name)
					continue
				}
				s.recorder.Eventf(s.Job.job, v1.EventTypeWarning, FailedCreateReason, "Error creating: %v", err)
				return k8sErrors.NewAggregate([]error{fmt.Errorf("Creating pod %v returned error.", createdPod.ObjectMeta.Name), err})
			}

			s.recorder.Eventf(s.Job.job, v1.EventTypeNormal, SuccessfulCreateReason, "Created pod: %v", createdPod.Name)
			continue
		}

		if err != nil {
			// TODO: handing this error
			continue
		}
	}

	return nil
}

// SyncServices will try to check current services for this TFReplicaSet and try to make it as desired.
func (s *MXReplicaSet) SyncServices() error {
	defer Exit(Enter("replicas.go $FN"))
	for index := int32(0); index < *s.Spec.Replicas; index++ {
		_, err := s.ClientSet.CoreV1().Services(s.Job.job.ObjectMeta.Namespace).Get(s.genName(index), meta_v1.GetOptions{})
		if err != nil && k8s_errors.IsNotFound(err) {
			log.Infof("Service: %v not found, create new one.", s.genName(index))
			// Create the service
			createdService, err := s.CreateServiceWithIndex(index)

			// If the service already exists do nothing.
			if err != nil {
				if k8s_errors.IsAlreadyExists(err) {
					log.Infof("Service: %v already exists.", s.genName(index))
					continue
				}
				s.recorder.Eventf(s.Job.job, v1.EventTypeWarning, FailedCreateReason, "Error creating: %v", err)
				return k8sErrors.NewAggregate([]error{fmt.Errorf("Creating Service %v returned error.", createdService.ObjectMeta.Name), err})
			}

			s.recorder.Eventf(s.Job.job, v1.EventTypeNormal, SuccessfulCreateReason, "Created Service: %v", createdService.Name)
			continue
		}

		if err != nil {
			// TODO: handing this error
			continue
		}
	}

	return nil
}

func (s *MXReplicaSet) genName(index int32) string {
	defer Exit(Enter("replicas.go $FN"))
	// Truncate tfjob name to 40 characters
	// The whole job name should be compliant with the DNS_LABEL spec, up to a max length of 63 characters
	// Thus genName(40 chars)-replicaType(6 chars)-runtimeId(4 chars)-index(4 chars), also leaving some spaces
	// See https://github.com/kubernetes/community/blob/master/contributors/design-proposals/architecture/identifiers.md
	return fmt.Sprintf("%v-%v-%v-%v", fmt.Sprintf("%.40s", s.Job.job.ObjectMeta.Name), strings.ToLower(string(s.Spec.MXReplicaType)), s.Job.job.Spec.RuntimeId, index)
}

func (s *MXReplicaSet) genPodName(index int32) string {
	defer Exit(Enter("replicas.go $FN"))
	// Generate a new pod name with random string
	return s.genName(index) + "-" + util.RandString(5)
}

func (s *MXReplicaSet) defaultPSConfigMapName() string {
	defer Exit(Enter("replicas.go $FN"))
	return fmt.Sprintf("cm-ps-%v", s.Job.job.Spec.RuntimeId)
}
