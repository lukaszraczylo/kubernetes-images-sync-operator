package shared

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	raczylocomv1 "raczylo.com/kubernetes-images-sync-operator/api/raczylo.com/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type K8sResource interface {
	GetPodSpec() *corev1.PodSpec
}

// Wrapper types
type DeploymentWrapper appsv1.Deployment
type JobWrapper batchv1.Job
type DaemonSetWrapper appsv1.DaemonSet
type CronJobWrapper batchv1.CronJob

// Implement the K8sResource interface for wrapper types
func (d *DeploymentWrapper) GetPodSpec() *corev1.PodSpec { return &d.Spec.Template.Spec }
func (j *JobWrapper) GetPodSpec() *corev1.PodSpec        { return &j.Spec.Template.Spec }
func (ds *DaemonSetWrapper) GetPodSpec() *corev1.PodSpec { return &ds.Spec.Template.Spec }
func (cj *CronJobWrapper) GetPodSpec() *corev1.PodSpec {
	return &cj.Spec.JobTemplate.Spec.Template.Spec
}

func processContainerName(containerName string) (Container, error) {
	cnt := Container{}
	parts := strings.Split(containerName, "@")
	if len(parts) > 2 {
		return cnt, fmt.Errorf("invalid container name format: %s", containerName)
	}
	imageAndTag := strings.Split(parts[0], ":")
	cnt.Image = imageAndTag[0]
	if len(imageAndTag) > 2 {
		return cnt, fmt.Errorf("invalid image:tag format: %s", parts[0])
	}
	if len(imageAndTag) == 2 {
		cnt.Tag = imageAndTag[1]
	}
	if len(parts) == 2 {
		shaParts := strings.SplitN(parts[1], ":", 2)
		if len(shaParts) != 2 || (shaParts[0] != "sha" && shaParts[0] != "sha256") {
			return cnt, fmt.Errorf("invalid SHA format: %s", parts[1])
		}
		cnt.Sha = parts[1]
	}
	cnt.FullName = containerName
	// if tag is empty and sha is empty - use tag 'latest'
	if cnt.Sha == "" && cnt.Tag == "" {
		cnt.Tag = "latest"
	}

	if cnt.Image == "" {
		return cnt, fmt.Errorf("image name is required")
	}
	return cnt, nil
}

func processContainers[T K8sResource](resource T, containersList *ContainersList) error {
	podSpec := resource.GetPodSpec()
	if podSpec == nil {
		return fmt.Errorf("nil PodSpec")
	}

	allContainers := append(podSpec.Containers, podSpec.InitContainers...)
	for _, container := range allContainers {
		if err := processContainer(container.Image, containersList); err != nil {
			return err
		}
	}

	for _, container := range podSpec.EphemeralContainers {
		if err := processContainer(container.EphemeralContainerCommon.Image, containersList); err != nil {
			return err
		}
	}

	return nil
}

// processContainer handles the processing of a single container image
func processContainer(image string, containersList *ContainersList) error {
	cnt, err := processContainerName(image)
	if err != nil {
		return fmt.Errorf("failed to process container name: %s - %w", image, err)
	}
	containersList.Containers = append(containersList.Containers, cnt)
	return nil
}

// listAndProcessResources is a generic function to list and process K8s resources
func ListAndProcessResources[T K8sResource, L client.ObjectList](ctx context.Context, r client.Client, list L, containersList *ContainersList) error {
	if err := r.List(ctx, list, &client.ListOptions{}); err != nil {
		return fmt.Errorf("failed to list resources: %w", err)
	}

	switch typedList := any(list).(type) {
	case *appsv1.DeploymentList:
		for i := range typedList.Items {
			if err := processContainers((*DeploymentWrapper)(&typedList.Items[i]), containersList); err != nil {
				return err
			}
		}
	case *batchv1.JobList:
		for i := range typedList.Items {
			if err := processContainers((*JobWrapper)(&typedList.Items[i]), containersList); err != nil {
				return err
			}
		}
	case *appsv1.DaemonSetList:
		for i := range typedList.Items {
			if err := processContainers((*DaemonSetWrapper)(&typedList.Items[i]), containersList); err != nil {
				return err
			}
		}
	case *batchv1.CronJobList:
		for i := range typedList.Items {
			if err := processContainers((*CronJobWrapper)(&typedList.Items[i]), containersList); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported list type: %T", list)
	}

	return nil
}

func SetupIndexers(mgr manager.Manager) error {
	return mgr.GetFieldIndexer().IndexField(context.Background(), &raczylocomv1.ClusterImage{}, "spec.exportName", func(rawObj client.Object) []string {
		clusterImage := rawObj.(*raczylocomv1.ClusterImage)
		return []string{clusterImage.Spec.ExportName}
	})
}
