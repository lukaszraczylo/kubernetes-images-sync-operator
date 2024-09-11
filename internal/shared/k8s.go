package shared

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	raczylocomv1 "github.com/lukaszraczylo/kubernetes-images-sync-operator/api/raczylo.com/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type K8sResource interface {
	GetPodSpec() *corev1.PodSpec
}

// Wrapper types
type (
	DeploymentWrapper appsv1.Deployment
	JobWrapper        batchv1.Job
	DaemonSetWrapper  appsv1.DaemonSet
	CronJobWrapper    batchv1.CronJob
)

// Implement the K8sResource interface for wrapper types
func (d *DeploymentWrapper) GetPodSpec() *corev1.PodSpec { return &d.Spec.Template.Spec }
func (j *JobWrapper) GetPodSpec() *corev1.PodSpec        { return &j.Spec.Template.Spec }
func (ds *DaemonSetWrapper) GetPodSpec() *corev1.PodSpec { return &ds.Spec.Template.Spec }
func (cj *CronJobWrapper) GetPodSpec() *corev1.PodSpec {
	return &cj.Spec.JobTemplate.Spec.Template.Spec
}

type ContainerCache struct {
	sync.RWMutex
	cache map[string]Container
}

var containerCache = &ContainerCache{
	cache: make(map[string]Container),
}

func (cc *ContainerCache) Get(key string) (Container, bool) {
	cc.RLock()
	defer cc.RUnlock()
	c, ok := cc.cache[key]
	return c, ok
}

func (cc *ContainerCache) Set(key string, value Container) {
	cc.Lock()
	defer cc.Unlock()
	cc.cache[key] = value
}

func ProcessContainerName(containerName string) (Container, error) {
	if cnt, ok := containerCache.Get(containerName); ok {
		return cnt, nil
	}

	cnt := Container{FullName: containerName}
	parts := strings.Split(containerName, "@")
	if len(parts) > 2 {
		return cnt, fmt.Errorf("invalid container name format: %s", containerName)
	}

	imageAndTag := strings.SplitN(parts[0], ":", 2)
	cnt.Image = imageAndTag[0]
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

	if cnt.Sha == "" && cnt.Tag == "" {
		cnt.Tag = "latest"
	}

	if cnt.Image == "" {
		return cnt, fmt.Errorf("image name is required")
	}

	containerCache.Set(containerName, cnt)
	return cnt, nil
}

func processContainers(ctx context.Context, resource K8sResource, namespace string, containersList *ContainersList) error {
	podSpec := resource.GetPodSpec()
	if podSpec == nil {
		return fmt.Errorf("nil PodSpec")
	}

	allContainers := append(podSpec.Containers, podSpec.InitContainers...)
	for _, container := range allContainers {
		if err := processContainer(ctx, container.Image, namespace, containersList); err != nil {
			return err
		}
	}

	for _, container := range podSpec.EphemeralContainers {
		if err := processContainer(ctx, container.EphemeralContainerCommon.Image, namespace, containersList); err != nil {
			return err
		}
	}

	return nil
}

func processContainer(ctx context.Context, image string, containerNamespace string, containersList *ContainersList) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		cnt, err := ProcessContainerName(image)
		if err != nil {
			return fmt.Errorf("failed to process container name: %s - %w", image, err)
		}
		cnt.ImageNamespace = containerNamespace
		containersList.Containers = append(containersList.Containers, cnt)
		return nil
	}
}

func ListAndProcessResources[T K8sResource, L client.ObjectList](ctx context.Context, r client.Client, list L, containersList *ContainersList) error {
	if err := r.List(ctx, list, &client.ListOptions{}); err != nil {
		return fmt.Errorf("failed to list resources: %w", err)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 1)
	semaphore := make(chan struct{}, 10) // Limit concurrent goroutines
	var mu sync.Mutex                    // Mutex to protect containersList

	processItem := func(item K8sResource, namespace string) {
		defer wg.Done()
		semaphore <- struct{}{}
		defer func() { <-semaphore }()

		localContainersList := &ContainersList{}
		if err := processContainers(ctx, item, namespace, localContainersList); err != nil {
			select {
			case errChan <- err:
			default:
			}
			return
		}

		// Safely append the local results to the main containersList
		mu.Lock()
		containersList.Containers = append(containersList.Containers, localContainersList.Containers...)
		mu.Unlock()
	}

	switch typedList := any(list).(type) {
	case *appsv1.DeploymentList:
		for i := range typedList.Items {
			wg.Add(1)
			go processItem((*DeploymentWrapper)(&typedList.Items[i]), typedList.Items[i].Namespace)
		}
	case *batchv1.JobList:
		for i := range typedList.Items {
			wg.Add(1)
			go processItem((*JobWrapper)(&typedList.Items[i]), typedList.Items[i].Namespace)
		}
	case *appsv1.DaemonSetList:
		for i := range typedList.Items {
			wg.Add(1)
			go processItem((*DaemonSetWrapper)(&typedList.Items[i]), typedList.Items[i].Namespace)
		}
	case *batchv1.CronJobList:
		for i := range typedList.Items {
			wg.Add(1)
			go processItem((*CronJobWrapper)(&typedList.Items[i]), typedList.Items[i].Namespace)
		}
	default:
		return fmt.Errorf("unsupported list type: %T", list)
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	select {
	case err := <-errChan:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func SetupIndexers(mgr manager.Manager) error {
	return wait.ExponentialBackoff(wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   2,
		Jitter:   0.1,
		Steps:    5,
	}, func() (bool, error) {
		err := mgr.GetFieldIndexer().IndexField(context.Background(), &raczylocomv1.ClusterImage{}, "spec.exportName", func(rawObj client.Object) []string {
			clusterImage := rawObj.(*raczylocomv1.ClusterImage)
			return []string{clusterImage.Spec.ExportName}
		})
		if err != nil {
			return false, nil
		}
		return true, nil
	})
}
