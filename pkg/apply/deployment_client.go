// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/bborbe/errors"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// ContainerImages maps a container name to its image.
type ContainerImages map[string]string

// String renders the images in a stable order, for result messages.
func (c ContainerImages) String() string {
	names := make([]string, 0, len(c))
	for name := range c {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+c[name])
	}
	return strings.Join(parts, ",")
}

// Equal reports whether both hold the same container images.
func (c ContainerImages) Equal(other ContainerImages) bool {
	if len(c) != len(other) {
		return false
	}
	for name, image := range c {
		if other[name] != image {
			return false
		}
	}
	return true
}

// DeploymentStatus is the subset of a Deployment's spec and status the
// post-apply health check reads.
type DeploymentStatus struct {
	Generation          int64
	ObservedGeneration  int64
	Replicas            int32
	UpdatedReplicas     int32
	AvailableReplicas   int32
	UnavailableReplicas int32
}

// Healthy reports whether the rollout finished with every replica on the new
// template and available. readyReplicas alone is not enough: a surviving old
// pod satisfies it while the new one crash-loops.
func (s DeploymentStatus) Healthy() bool {
	return s.ObservedGeneration >= s.Generation &&
		s.UpdatedReplicas == s.Replicas &&
		s.AvailableReplicas == s.Replicas &&
		s.UnavailableReplicas == 0
}

//counterfeiter:generate -o ../../mocks/deployment-client.go --fake-name DeploymentClient . DeploymentClient

// DeploymentClient reads and restores the Deployments of the stage namespace.
type DeploymentClient interface {
	// Images returns the Deployment's container images, or nil when the
	// Deployment does not exist yet.
	Images(ctx context.Context, name string) (ContainerImages, error)
	Status(ctx context.Context, name string) (*DeploymentStatus, error)
	SetImages(ctx context.Context, name string, images ContainerImages) error
}

// NewDeploymentClient returns a DeploymentClient scoped to one namespace.
func NewDeploymentClient(clientset kubernetes.Interface, namespace string) DeploymentClient {
	return &deploymentClient{clientset: clientset, namespace: namespace}
}

type deploymentClient struct {
	clientset kubernetes.Interface
	namespace string
}

func (d *deploymentClient) get(ctx context.Context, name string) (*appsv1.Deployment, error) {
	deployment, err := d.clientset.AppsV1().
		Deployments(d.namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "get deployment '%s/%s' failed", d.namespace, name)
	}
	return deployment, nil
}

func (d *deploymentClient) Images(ctx context.Context, name string) (ContainerImages, error) {
	deployment, err := d.get(ctx, name)
	// IsNotFound unwraps with errors.As, so the wrap in get does not hide it.
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	images := ContainerImages{}
	for _, container := range deployment.Spec.Template.Spec.Containers {
		images[container.Name] = container.Image
	}
	return images, nil
}

func (d *deploymentClient) Status(ctx context.Context, name string) (*DeploymentStatus, error) {
	deployment, err := d.get(ctx, name)
	if err != nil {
		return nil, err
	}
	replicas := int32(1)
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}
	return &DeploymentStatus{
		Generation:          deployment.Generation,
		ObservedGeneration:  deployment.Status.ObservedGeneration,
		Replicas:            replicas,
		UpdatedReplicas:     deployment.Status.UpdatedReplicas,
		AvailableReplicas:   deployment.Status.AvailableReplicas,
		UnavailableReplicas: deployment.Status.UnavailableReplicas,
	}, nil
}

type containerImagePatch struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

// SetImages writes the given images back with a strategic merge patch. The
// patch merges containers by name, so it touches nothing but the image fields.
func (d *deploymentClient) SetImages(
	ctx context.Context,
	name string,
	images ContainerImages,
) error {
	containers := make([]containerImagePatch, 0, len(images))
	for containerName, image := range images {
		containers = append(containers, containerImagePatch{Name: containerName, Image: image})
	}
	sort.Slice(containers, func(i, j int) bool { return containers[i].Name < containers[j].Name })
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": containers,
				},
			},
		},
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return errors.Wrapf(ctx, err, "marshal image patch failed")
	}
	if _, err := d.clientset.AppsV1().
		Deployments(d.namespace).
		Patch(ctx, name, types.StrategicMergePatchType, body, metav1.PatchOptions{}); err != nil {
		return errors.Wrapf(ctx, err, "patch deployment '%s/%s' failed", d.namespace, name)
	}
	return nil
}
