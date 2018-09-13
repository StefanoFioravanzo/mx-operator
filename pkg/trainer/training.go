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

// Package trainer is to manage TensorFlow training jobs.
package trainer

import (
	"fmt"
	"reflect"
	"strings"

	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	"github.com/kubeflow/tf-operator/pkg/apis/mxnet/helper"
	mxv1alpha1 "github.com/kubeflow/tf-operator/pkg/apis/mxnet/v1alpha1"
	"github.com/kubeflow/tf-operator/pkg/apis/mxnet/validation"
	mxjobclient "github.com/kubeflow/tf-operator/pkg/client/clientset/versioned"
	//"github.com/kubeflow/tf-operator/pkg/client/clientset/versioned/scheme"
	"github.com/kubeflow/tf-operator/pkg/util"

	"github.com/golang/protobuf/proto"

	"github.com/sabhiram/go-tracey"
)

var Exit, Enter = tracey.New(nil)

// TODO(jlewi): We should switch a New pattern and make trainingJob private so we can
// ensure correctness on creation.
type TrainingJob struct {
	job *mxv1alpha1.MXJob

	KubeCli kubernetes.Interface

	recorder record.EventRecorder

	Replicas []*MXReplicaSet

	mxjobclient mxjobclient.Interface

	// in memory state of the job.
	// status is the source of truth after job struct is materialized. Changes to the status to be persisted
	// should be made here.
	status mxv1alpha1.MXJobStatus

	memberCounter int
}

// ClusterSpec represents a cluster TensorFlow specification.
// https://www.tensorflow.org/deploy/distributed#create_a_tftrainclusterspec_to_describe_the_cluster
// It is a map from job names to network addresses.
type ClusterSpec map[string][]string

type TaskSpec struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

func initJob(kubeCli kubernetes.Interface, mxjobclient mxjobclient.Interface, recorder record.EventRecorder, job *mxv1alpha1.MXJob) (*TrainingJob, error) {
	defer Exit(Enter("training.go: $FN"))
	j := &TrainingJob{
		KubeCli:     kubeCli,
		mxjobclient: mxjobclient,
		recorder:    recorder,
		Replicas:    make([]*MXReplicaSet, 0),
		job:         job,
		status:      *job.Status.DeepCopy(),
	}

	return j, nil
}

func NewJob(kubeCli kubernetes.Interface, mxjobclient mxjobclient.Interface, recorder record.EventRecorder, job *mxv1alpha1.MXJob, config *mxv1alpha1.ControllerConfig) (*TrainingJob, error) {
	defer Exit(Enter("training.go: $FN"))
	j, err := initJob(kubeCli, mxjobclient, recorder, job)
	if err != nil {
		return nil, err
	}

	return j, nil
}

func (j *TrainingJob) UID() types.UID {
	defer Exit(Enter("training.go: $FN"))
	return j.job.ObjectMeta.UID
}

func (j *TrainingJob) ClusterSpec() ClusterSpec {
	defer Exit(Enter("training.go: $FN"))
	clusterSpec := make(ClusterSpec)

	for _, p := range j.Replicas {
		replicaNames := make([]string, 0, *p.Spec.Replicas)

		for i := int32(0); i < *p.Spec.Replicas; i++ {
			replicaNames = append(replicaNames, fmt.Sprintf("%v:%v", p.genName(i), *p.Spec.MXPort))
		}

		clusterSpec[strings.ToLower(string(p.Spec.MXReplicaType))] = replicaNames
	}

	return clusterSpec
}

// deleteResources deletes the replicas it it was created
func (j *TrainingJob) deleteResources() error {
	defer Exit(Enter("training.go: $FN"))
	for _, r := range j.Replicas {
		if err := r.Delete(); err != nil {
			return err
		}
	}

	return nil
}

func (j *TrainingJob) GetStatus() (mxv1alpha1.State, []*mxv1alpha1.MXReplicaStatus, error) {
	defer Exit(Enter("training.go: $FN"))
	chief := j.job.Spec.TerminationPolicy.Chief
	chiefState := mxv1alpha1.ReplicaStateUnknown

	state := mxv1alpha1.StateUnknown
	replicaStatuses := make([]*mxv1alpha1.MXReplicaStatus, 0)

	// The state for each replica.
	// TODO(jlewi): We will need to modify this code if we want to allow multiples of a given type of replica.
	replicaSetStates := make(map[mxv1alpha1.MXReplicaType]mxv1alpha1.ReplicaState)

	for _, r := range j.Replicas {
		rStatus, err := r.GetStatus()
		if err != nil {
			log.Errorf("GetStatus() for %v returned error; %v", r.Spec.MXReplicaType, err)
		}

		replicaSetStates[r.Spec.MXReplicaType] = rStatus.State

		replicaStatuses = append(replicaStatuses, &rStatus)

		if string(r.Spec.MXReplicaType) == chief.ReplicaName {
			chiefState = r.GetSingleReplicaStatus(int32(chief.ReplicaIndex))
		}
	}

	if chiefState == mxv1alpha1.ReplicaStateRunning {
		state = mxv1alpha1.StateRunning
	} else if chiefState == mxv1alpha1.ReplicaStateFailed {
		state = mxv1alpha1.StateFailed
	} else if chiefState == mxv1alpha1.ReplicaStateSucceeded {
		state = mxv1alpha1.StateSucceeded
	}

	return state, replicaStatuses, nil
}

// isRetryableTerminationState returns true if a container terminated in a state
// that we consider retryable.
func isRetryableTerminationState(s *v1.ContainerStateTerminated) bool {
	defer Exit(Enter("training.go: $FN"))
	// TODO(jlewi): Need to match logic in
	// https://cs.corp.google.com/piper///depot/google3/cloud/ml/beta/job/training_job_state_util.cc?l=88
	if s.Reason == "OOMKilled" {
		// If the user's process causes an OOM and Docker kills the container,
		// the termination reason of ContainerState will be specified to
		// 'OOMKilled'. In this case, we can't assume this to be a retryable error.
		//
		// This check should happen before checking the termination log, since
		// if the container terminated with an OOM, the termination log may not
		// be written.
		return false
	}

	// TODO(jlewi): Should we use the exit code reported in the termination
	// log message and not the ExitCode reported by the container.

	if s.ExitCode >= 0 && s.ExitCode <= 127 {
		// For the exit_code in [0, 127]:
		//   0 means success,
		//   1 - 127 corresponds to permanent user errors.
		// We don't want to retry for both cases.
		// More info about exit status can be found in:
		// https://www.gnu.org/software/bash/manual/html_node/Exit-Status.html
		return false
	}

	// For the remaining cases that exit_code from workers that doesn't
	// fall into [0, 127]. They can be:
	//   137 corresponds to SIGKILL,
	//   143 corresponds to SIGTERM,
	//   other values that have undefined behavior.
	// We treat them as internal errors for now and all the internal errors
	// will be retired.
	return true
}

func (j *TrainingJob) masterName() string {
	defer Exit(Enter("training.go: $FN"))
	return fmt.Sprintf("master-%v-0", j.job.Spec.RuntimeId)
}

// setup the training job.
func (j *TrainingJob) setup(config *mxv1alpha1.ControllerConfig) {
	defer Exit(Enter("training.go: $FN"))
	err := func() error {
		// If the job has already started we shouldn't set it up again.
		if j.status.Phase != mxv1alpha1.MXJobPhaseNone {
			log.Warningf("Job %v has already been setup.", j.name())
			return nil
		}

		// Set defaults.
		//scheme.Scheme.Default(j.job)

		// SetDefaults_MXJob sets any unspecified values to defaults
		SetDefaultsMXJob := func(obj *mxv1alpha1.MXJob) {
			c := &obj.Spec

			if c.MXImage == "" {
				c.MXImage = mxv1alpha1.DefaultMXImage
			}

			// Check that each replica has a TensorFlow container.
			for _, r := range c.ReplicaSpecs {

				if r.MXPort == nil {
					r.MXPort = proto.Int32(mxv1alpha1.MXPort)
				}

				if string(r.MXReplicaType) == "" {
					r.MXReplicaType= mxv1alpha1.SCHEDULER
				}

				if r.Replicas == nil {
					r.Replicas = proto.Int32(mxv1alpha1.Replicas)
				}
			}
			if c.TerminationPolicy == nil {
				c.TerminationPolicy = &mxv1alpha1.TerminationPolicySpec{
					Chief: &mxv1alpha1.ChiefSpec{
						ReplicaName:  "SCHEDULER",
						ReplicaIndex: 0,
					},
				}
			}

		}
		SetDefaultsMXJob(j.job)

		err := validation.ValidateTFJobSpec(&j.job.Spec)
		if err != nil {
			return fmt.Errorf("invalid job spec: %v", err)
		}

		if err := helper.ConfigureAcceleratorsForTFJobSpec(&j.job.Spec, config.Accelerators); err != nil {
			return fmt.Errorf("ConfigureAccelerators(...) error; %v", err)
		}

		if j.job.Spec.RuntimeId == "" {
			j.job.Spec.RuntimeId = util.RandString(4)
		}
		return nil
	}()

	if err != nil {
		j.status.Reason = err.Error()
		j.status.Phase = mxv1alpha1.MXJobPhaseFailed
		j.status.State = mxv1alpha1.StateFailed
	} else {
		j.status.Phase = mxv1alpha1.MXJobPhaseCreating
		j.status.State = mxv1alpha1.StateRunning
	}
}

// setup Replicas. This creates in memory data structures corresponding to the replicas.
func (j *TrainingJob) setupReplicas() error {
	defer Exit(Enter("training.go: $FN"))
	if len(j.Replicas) != len(j.job.Spec.ReplicaSpecs) {
		j.Replicas = make([]*MXReplicaSet, 0, len(j.job.Spec.ReplicaSpecs))
		for _, t := range j.job.Spec.ReplicaSpecs {
			r, err := NewTFReplicaSet(j.KubeCli, j.recorder, *t, j)
			if err != nil {
				return err
			}
			j.Replicas = append(j.Replicas, r)
		}
	}

	return nil
}

func (j *TrainingJob) Delete() {
	defer Exit(Enter("training.go: $FN"))
	// TODO(jlewi): Delete is what should cause us to delete the Pods.
	// we shouldn't delete the pods when the jobs finish because leaving the pods
	// allows us to get the logs from the pods after the job finishes.
	//
	log.Infof("TFJob %v deleted by the user", j.fullname())
	// TODO(jlewi): This logic is probably insufficient.
	if j.job.Status.Phase != mxv1alpha1.MXJobPhaseCleanUp {
		j.status.Phase = mxv1alpha1.MXJobPhaseCleanUp
	}

	// TODO(jlewi): Does it make sense to explicitly delete the resources? Should
	// we just rely on K8s garbage collection to delete the resources before
	// deleting TFJob?
	if cErr := j.deleteResources(); cErr != nil {
		log.Errorf("trainingJob.deleteResources() error; %v", cErr)
	}
}

// updateCRDStatus updates the job status based on TraingingJob.status.
func (j *TrainingJob) updateCRDStatus() error {
	defer Exit(Enter("training.go: $FN"))
	// If the status hasn't changed then there's no reason to update the CRD.
	if reflect.DeepEqual(j.job.Status, j.status) {
		return nil
	}

	newJob := j.job
	newJob.Status = j.status
	newJob, err := j.mxjobclient.FioravanzoV1alpha1().MXJobs(j.job.ObjectMeta.Namespace).Update(newJob)
	if err != nil {
		return err
	}

	j.job = newJob

	return nil
}

// reconcile tries to get the job into the desired state.
func (j *TrainingJob) Reconcile(config *mxv1alpha1.ControllerConfig) error {
	defer Exit(Enter("training.go: $FN"))
	if j.job.Status.Phase == mxv1alpha1.MXJobPhaseNone {
		// The job hasn't been setup.
		j.setup(config)

		if err := j.updateCRDStatus(); err != nil {
			log.Warningf("failed to update CRD status: %v", err)
			return err
		}
	}

	// setupreplicas initializes data structures inside TrainingJob representing the replicas.
	// These are go-lang structures which aren't preserved in the APIServer. So we always need to call setupReplicas
	// unlike setup which only needs to be called once during the lifecycle of the job.
	if err := j.setupReplicas(); err != nil {
		log.Errorf("failed to create replicas: %v", err)
		j.status.Reason = fmt.Sprintf("Could not create in memory datastructures; %v", err)
		if uErr := j.updateCRDStatus(); err != nil {
			log.Warningf("Job %v; failed to update status error: %v", j.job.ObjectMeta.Name, uErr)
		}
		return err
	}

	// sync pods
	for _, rc := range j.Replicas {
		err := rc.SyncPods()
		if err != nil {
			log.Errorf("SyncPods error: %v", err)
		}
	}

	// sync services
	for _, rc := range j.Replicas {
		err := rc.SyncServices()
		if err != nil {
			log.Errorf("SyncServices error: %v", err)
		}
	}

	if err := j.updateCRDStatus(); err != nil {
		log.Warningf("Job %v; failed to update status error: %v", j.job.ObjectMeta.Name, err)
		return err
	}

	// Call GetStatus in each reconcile loop
	state, replicaStatuses, err := j.GetStatus()

	j.status.ReplicaStatuses = replicaStatuses
	if err != nil {
		log.Errorf("GetStatus() for job %v returned error: %v", j.job.ObjectMeta.Name, err)
		return err
	}

	// TODO(jlewi): We should update the Phase if we detect the job is done.
	if state == mxv1alpha1.StateFailed {
		log.Errorf("Master failed Job: %v.", j.job.ObjectMeta.Name)
		j.status.Phase = mxv1alpha1.MXJobPhaseDone
		j.status.State = mxv1alpha1.StateFailed
	} else if state == mxv1alpha1.StateSucceeded {
		log.Infof("Master succeeded Job: %v.", j.job.ObjectMeta.Name)
		j.status.Phase = mxv1alpha1.MXJobPhaseDone
		j.status.State = mxv1alpha1.StateSucceeded
	} else if state == mxv1alpha1.StateRunning {
		log.Infof("Master running Job: %v.", j.job.ObjectMeta.Name)
		j.status.Phase = mxv1alpha1.MXJobPhaseRunning
		j.status.State = mxv1alpha1.StateRunning
	} else {
		log.Infof("Job %v status=%v", j.job.ObjectMeta.Name, util.Pformat(j.status))
	}

	// If the phase changed we should update the CRD.
	if err := j.updateCRDStatus(); err != nil {
		log.Warningf("Job %v, failed to update CRD status error: %v", j.job.ObjectMeta.Name, err)
		return err
	}

	if j.job.Status.Phase == mxv1alpha1.MXJobPhaseCleanUp {
		if cErr := j.deleteResources(); cErr != nil {
			log.Errorf("Job %v trainingJob.Delete() error; %v", j.job.ObjectMeta.Name, cErr)
		}
		// j.status.SetPhase(spec.TFJobPhaseDone)
		// Return from run because we want to stop reconciling the object.
		return nil
	}

	// updateCRDStatus will update the status of the CRD with c.Status if c.Status
	// doesn't match c.Cluster.status. So you can change c.Status in order to propagate
	// changes to the CRD status.
	if err := j.updateCRDStatus(); err != nil {
		log.Warningf("Job %v; failed to update CRD status error: %v", j.job.ObjectMeta.Name, err)
		return err
	}

	return nil
}

func (j *TrainingJob) name() string {
	defer Exit(Enter("training.go: $FN"))
	return j.job.ObjectMeta.GetName()
}

// fullname returns the namespace and name for the job.
func (j *TrainingJob) fullname() string {
	defer Exit(Enter("training.go: $FN"))
	return j.job.ObjectMeta.GetNamespace() + ":" + j.job.ObjectMeta.GetName()
}

func (j *TrainingJob) SchedulerName() string {
	defer Exit(Enter("training.go: $FN"))
	return j.job.Spec.SchedulerName
}
