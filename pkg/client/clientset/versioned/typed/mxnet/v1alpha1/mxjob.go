/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/kubeflow/tf-operator/pkg/apis/mxnet/v1alpha1"
	scheme "github.com/kubeflow/tf-operator/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// MXJobsGetter has a method to return a MXJobInterface.
// A group's client should implement this interface.
type MXJobsGetter interface {
	MXJobs(namespace string) MXJobInterface
}

// MXJobInterface has methods to work with MXJob resources.
type MXJobInterface interface {
	Create(*v1alpha1.MXJob) (*v1alpha1.MXJob, error)
	Update(*v1alpha1.MXJob) (*v1alpha1.MXJob, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1alpha1.MXJob, error)
	List(opts v1.ListOptions) (*v1alpha1.MXJobList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.MXJob, err error)
	MXJobExpansion
}

// mXJobs implements MXJobInterface
type mXJobs struct {
	client rest.Interface
	ns     string
}

// newMXJobs returns a MXJobs
func newMXJobs(c *KubeflowV1alpha1Client, namespace string) *mXJobs {
	return &mXJobs{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the mXJob, and returns the corresponding mXJob object, and an error if there is any.
func (c *mXJobs) Get(name string, options v1.GetOptions) (result *v1alpha1.MXJob, err error) {
	result = &v1alpha1.MXJob{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("mxjobs").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of MXJobs that match those selectors.
func (c *mXJobs) List(opts v1.ListOptions) (result *v1alpha1.MXJobList, err error) {
	result = &v1alpha1.MXJobList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("mxjobs").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested mXJobs.
func (c *mXJobs) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("mxjobs").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a mXJob and creates it.  Returns the server's representation of the mXJob, and an error, if there is any.
func (c *mXJobs) Create(mXJob *v1alpha1.MXJob) (result *v1alpha1.MXJob, err error) {
	result = &v1alpha1.MXJob{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("mxjobs").
		Body(mXJob).
		Do().
		Into(result)
	return
}

// Update takes the representation of a mXJob and updates it. Returns the server's representation of the mXJob, and an error, if there is any.
func (c *mXJobs) Update(mXJob *v1alpha1.MXJob) (result *v1alpha1.MXJob, err error) {
	result = &v1alpha1.MXJob{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("mxjobs").
		Name(mXJob.Name).
		Body(mXJob).
		Do().
		Into(result)
	return
}

// Delete takes name of the mXJob and deletes it. Returns an error if one occurs.
func (c *mXJobs) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("mxjobs").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *mXJobs) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("mxjobs").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched mXJob.
func (c *mXJobs) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.MXJob, err error) {
	result = &v1alpha1.MXJob{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("mxjobs").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
