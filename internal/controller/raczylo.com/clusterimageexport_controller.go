package raczylocom

import (
	"context"
	"crypto/md5"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	raczylocomv1 "github.com/lukaszraczylo/kubernetes-images-sync-operator/api/raczylo.com/v1"
	shared "github.com/lukaszraczylo/kubernetes-images-sync-operator/internal/shared"
)

// ClusterImageExportReconciler reconciles a ClusterImageExport object
type ClusterImageExportReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	podAnnotations map[string]string
}

func (r *ClusterImageExportReconciler) InjectPodAnnotations(annotations map[string]string) {
	r.podAnnotations = annotations
}

// +kubebuilder:rbac:groups=raczylo.com,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=raczylo.com,resources=*/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=raczylo.com,resources=*/finalizers,verbs=update
// additional RBAC rules
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete

const clusterImageExportFinalizer = "raczylo.com/clusterimageexport-finalizer"

func (r *ClusterImageExportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	// l.Info("Reconciling ClusterImageExport")

	// Fetch the ClusterImageExport instance
	clusterImageExport := &raczylocomv1.ClusterImageExport{}
	if err := r.Get(ctx, req.NamespacedName, clusterImageExport); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !clusterImageExport.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, clusterImageExport)
	}

	// Add finalizer and creation timestamp annotation if they don't exist
	needsUpdate := false
	if !controllerutil.ContainsFinalizer(clusterImageExport, clusterImageExportFinalizer) {
		controllerutil.AddFinalizer(clusterImageExport, clusterImageExportFinalizer)
		needsUpdate = true
	}

	// Add creation timestamp annotation if it doesn't exist
	if clusterImageExport.Annotations == nil {
		clusterImageExport.Annotations = make(map[string]string)
	}
	if _, exists := clusterImageExport.Annotations["export.raczylo.com/creation-timestamp"]; !exists {
		clusterImageExport.Annotations["export.raczylo.com/creation-timestamp"] = clusterImageExport.CreationTimestamp.String()
		needsUpdate = true
	}

	if needsUpdate {
		if err := r.Update(ctx, clusterImageExport); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Proceed with reconciliation logic
	// Get list of all images to be exported
	fullImagesList, err := r.listImagesInCluster(ctx, l, clusterImageExport)
	if err != nil {
		l.Error(err, "unable to list images in the cluster")
		return ctrl.Result{}, err
	}

	// Add additional images if specified
	if len(clusterImageExport.Spec.AdditionalImages) > 0 {
		for _, image := range clusterImageExport.Spec.AdditionalImages {
			img, err := shared.ProcessContainerName(image)
			if err != nil {
				l.Error(err, "unable to process additional image", "image", image)
				continue
			}
			fullImagesList.Containers = append(fullImagesList.Containers, img)
		}
	}

	// Update total image count and status
	totalImages := len(fullImagesList.Containers)
	if err := r.updateStatusWithRetry(ctx, clusterImageExport, func(export *raczylocomv1.ClusterImageExport) error {
		if export.Status.Progress == "" {
			export.Status.Progress = shared.STATUS_PENDING
		}
		if export.Status.Progress == shared.STATUS_PENDING {
			export.Status.Progress = shared.STATUS_RUNNING
		}
		export.Status.TotalImages = totalImages
		return nil
	}); err != nil {
		l.Error(err, "unable to update ClusterImageExport status")
		return ctrl.Result{}, err
	}

	for _, image := range fullImagesList.Containers {
		// Include creation timestamp in the hash to differentiate between exports with the same name
		nameHash := fmt.Sprintf("%x", md5.Sum([]byte(clusterImageExport.Name+image.Image+image.Tag+image.Sha+
			clusterImageExport.Annotations["export.raczylo.com/creation-timestamp"])))[:14]

		// Check if the ClusterImage already exists
		clusterImage := &raczylocomv1.ClusterImage{}
		err := r.Get(ctx, client.ObjectKey{Namespace: clusterImageExport.Namespace, Name: nameHash}, clusterImage)
		if err == nil {
			// ClusterImage exists, check its status
			if clusterImage.Status.Progress == shared.STATUS_FAILED {
				if err := r.updateStatusWithRetry(ctx, clusterImageExport, func(export *raczylocomv1.ClusterImageExport) error {
					export.Status.Progress = shared.STATUS_FAILED
					return nil
				}); err != nil {
					l.Error(err, "unable to update ClusterImageExport status to FAILED")
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}
			continue
		} else if !errors.IsNotFound(err) {
			l.Error(err, "unable to get ClusterImage")
			return ctrl.Result{}, err
		}

		// Create a new ClusterImage
		newClusterImage := &raczylocomv1.ClusterImage{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nameHash,
				Namespace: clusterImageExport.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: clusterImageExport.APIVersion,
						Kind:       clusterImageExport.Kind,
						Name:       clusterImageExport.Name,
						UID:        clusterImageExport.UID,
						Controller: pointer.Bool(true),
					},
				},
			},
			Spec: raczylocomv1.ClusterImageSpec{
				Image:            image.Image,
				Tag:              image.Tag,
				Sha:              image.Sha,
				FullName:         image.FullName,
				ImageNamespace:   image.ImageNamespace,
				Storage:          clusterImageExport.Spec.Storage.StorageTarget,
				ExportName:       clusterImageExport.Name,
				ExportPath:       clusterImageExport.Spec.BasePath,
				JobAnnotations:   clusterImageExport.Spec.JobAnnotations,
				ImagePullSecrets: clusterImageExport.Spec.ImagePullSecrets,
			},
		}

		if err := r.Create(ctx, newClusterImage); err != nil {
			l.Error(err, "unable to create ClusterImage", "image", image)
			return ctrl.Result{}, err
		}
	}

	// Check completion status and update counts
	completedCount := 0
	clusterImageList := &raczylocomv1.ClusterImageList{}
	if err := r.List(ctx, clusterImageList, client.InNamespace(clusterImageExport.Namespace), 
		client.MatchingFields{"spec.exportName": clusterImageExport.Name}); err != nil {
		l.Error(err, "unable to list ClusterImages")
		return ctrl.Result{}, err
	}

	for _, ci := range clusterImageList.Items {
		if ci.Status.Progress == shared.STATUS_SUCCESS || ci.Status.Progress == shared.STATUS_PRESENT {
			completedCount++
		}
	}

	// Update status with completion info
	if completedCount == totalImages && totalImages > 0 {
		if err := r.updateStatusWithRetry(ctx, clusterImageExport, func(export *raczylocomv1.ClusterImageExport) error {
			export.Status.Progress = shared.STATUS_SUCCESS
			export.Status.CompletedImages = completedCount
			return nil
		}); err != nil {
			l.Error(err, "unable to update ClusterImageExport status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	} else {
		if err := r.updateStatusWithRetry(ctx, clusterImageExport, func(export *raczylocomv1.ClusterImageExport) error {
			export.Status.CompletedImages = completedCount
			return nil
		}); err != nil {
			l.Error(err, "unable to update ClusterImageExport status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{Requeue: true}, nil
}

// updateStatusWithRetry attempts to update the status of a ClusterImageExport with retries
func (r *ClusterImageExportReconciler) updateStatusWithRetry(ctx context.Context, clusterImageExport *raczylocomv1.ClusterImageExport, updateFn func(*raczylocomv1.ClusterImageExport) error) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get the latest version of the resource
		latest := &raczylocomv1.ClusterImageExport{}
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: clusterImageExport.Namespace,
			Name:      clusterImageExport.Name,
		}, latest); err != nil {
			return err
		}

		// Apply the update function to the latest version
		if err := updateFn(latest); err != nil {
			return err
		}

		// Update the status
		return r.Status().Update(ctx, latest)
	})
}

func (r *ClusterImageExportReconciler) checkAllClusterImagesCompleted(ctx context.Context, clusterImageExport *raczylocomv1.ClusterImageExport) (bool, error) {
	clusterImageList := &raczylocomv1.ClusterImageList{}
	if err := r.List(ctx, clusterImageList, client.InNamespace(clusterImageExport.Namespace), client.MatchingFields{"spec.exportName": clusterImageExport.Name}); err != nil {
		return false, err
	}

	for _, ci := range clusterImageList.Items {
		if ci.Status.Progress != shared.STATUS_SUCCESS && ci.Status.Progress != shared.STATUS_PRESENT {
			return false, nil
		}
	}

	return true, nil
}

// SetupWithManager sets up the controller with the Manager.

func (r *ClusterImageExportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&raczylocomv1.ClusterImageExport{}).
		Owns(&raczylocomv1.ClusterImage{}).
		Complete(r)
}

func (r *ClusterImageExportReconciler) listImagesInCluster(ctx context.Context, l logr.Logger, clusterImageExport *raczylocomv1.ClusterImageExport) (shared.ContainersList, error) {
	containersList := shared.ContainersList{}
	if err := shared.ListAndProcessResources[*shared.DeploymentWrapper](ctx, r.Client, &appsv1.DeploymentList{}, &containersList); err != nil {
		return shared.ContainersList{}, err
	}
	if err := shared.ListAndProcessResources[*shared.JobWrapper](ctx, r.Client, &batchv1.JobList{}, &containersList); err != nil {
		return shared.ContainersList{}, err
	}
	if err := shared.ListAndProcessResources[*shared.DaemonSetWrapper](ctx, r.Client, &appsv1.DaemonSetList{}, &containersList); err != nil {
		return shared.ContainersList{}, err
	}
	if err := shared.ListAndProcessResources[*shared.CronJobWrapper](ctx, r.Client, &batchv1.CronJobList{}, &containersList); err != nil {
		return shared.ContainersList{}, err
	}

	if len(clusterImageExport.Spec.Includes) > 0 {
		containersList = shared.IncludeOnlyImages(containersList, clusterImageExport.Spec.Includes)
	}

	if len(clusterImageExport.Spec.Excludes) > 0 {
		containersList = shared.RemoveExcludedImages(containersList, clusterImageExport.Spec.Excludes)
	}

	if len(clusterImageExport.Spec.Namespaces) > 0 {
		containersList = shared.FilterOnlyFromNamespaces(containersList, clusterImageExport.Spec.Namespaces)
	}

	if len(clusterImageExport.Spec.ExcludedNamespaces) > 0 {
		containersList = shared.FilterOutWholeNamespaces(containersList, clusterImageExport.Spec.ExcludedNamespaces)
	}

	containersList = shared.RemoveDuplicates(containersList)
	// l.Info("List of containers in the cluster", "containers", containersList)

	return containersList, nil
}

func (r *ClusterImageExportReconciler) handleDeletion(ctx context.Context, clusterImageExport *raczylocomv1.ClusterImageExport) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(clusterImageExport, clusterImageExportFinalizer) {
		// Delete all associated ClusterImages first
		if err := r.deleteAssociatedClusterImages(ctx, clusterImageExport); err != nil {
			l.Error(err, "Failed to delete associated ClusterImages")
			// Continue with deletion even if cleanup fails
		}

		// Create or recreate cleanup job
		jobName := "cleanup-" + shared.NormalizeImageName(clusterImageExport.Name)
		existingJob := &batchv1.Job{}
		err := r.Get(ctx, client.ObjectKey{
			Namespace: clusterImageExport.Namespace,
			Name:      jobName,
		}, existingJob)

		if err == nil {
			// Job exists, delete it first
			deletePolicy := metav1.DeletePropagationForeground
			deleteOptions := client.DeleteOptions{
				PropagationPolicy: &deletePolicy,
			}
			if err := r.Delete(ctx, existingJob, &deleteOptions); err != nil && !errors.IsNotFound(err) {
				l.Error(err, "Failed to delete existing cleanup job")
				return ctrl.Result{Requeue: true}, nil
			}
			l.Info("Successfully deleted existing cleanup job", "job", jobName)
			// Requeue to wait for job deletion to complete
			return ctrl.Result{Requeue: true}, nil
		} else if !errors.IsNotFound(err) {
			// Unexpected error
			l.Error(err, "Error checking for existing cleanup job")
			return ctrl.Result{Requeue: true}, nil
		}

		// Create new cleanup job
		if err := r.runCleanupJob(ctx, clusterImageExport); err != nil {
			l.Error(err, "Failed to create cleanup job")
			return ctrl.Result{Requeue: true}, nil
		}

		// Only remove finalizer after cleanup job is created successfully
		controllerutil.RemoveFinalizer(clusterImageExport, clusterImageExportFinalizer)
		if err := r.Update(ctx, clusterImageExport); err != nil {
			if errors.IsNotFound(err) {
				// Object is already gone, which is fine
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *ClusterImageExportReconciler) deleteAssociatedClusterImages(ctx context.Context, clusterImageExport *raczylocomv1.ClusterImageExport) error {
	l := log.FromContext(ctx)

	// List all ClusterImages associated with this export
	clusterImageList := &raczylocomv1.ClusterImageList{}
	if err := r.List(ctx, clusterImageList, client.InNamespace(clusterImageExport.Namespace),
		client.MatchingFields{"spec.exportName": clusterImageExport.Name}); err != nil {
		return fmt.Errorf("failed to list ClusterImages: %w", err)
	}

	// Delete each ClusterImage
	for _, ci := range clusterImageList.Items {
		if err := r.Delete(ctx, &ci); err != nil && !errors.IsNotFound(err) {
			l.Error(err, "Failed to delete ClusterImage", "name", ci.Name)
			// Continue deleting other ClusterImages even if one fails
		}
	}

	return nil
}

func (r *ClusterImageExportReconciler) runCleanupJob(ctx context.Context, clusterImageExport *raczylocomv1.ClusterImageExport) error {
	l := log.FromContext(ctx)
	normalisedImageName := "cleanup-" + shared.NormalizeImageName(clusterImageExport.Name)

	defaultCommands := []string{}

	if clusterImageExport.Spec.Storage.StorageTarget == shared.STORAGE_S3 {
		s3Params := shared.SetupS3Params(clusterImageExport.Spec.Storage.S3)
		additionalCommands := []string{
			"./cleanup.py " + strings.Join(s3Params, " ") + " 's3://" + clusterImageExport.Spec.Storage.S3.Bucket + clusterImageExport.Spec.BasePath + "/" + clusterImageExport.ObjectMeta.Name + "/'",
		}
		defaultCommands = append(defaultCommands, additionalCommands...)
	} else if clusterImageExport.Spec.Storage.StorageTarget == shared.STORAGE_FILE {
		additionalCommands := []string{
			"./cleanup.py" + "'" + clusterImageExport.Spec.BasePath + "/" + clusterImageExport.ObjectMeta.Name + "/'",
		}
		defaultCommands = append(defaultCommands, additionalCommands...)
	}

	// Set up the cleanup job with retry limits and TTL
	backoffLimit := int32(2) // 3 total attempts (initial + 2 retries)
	ttlSecondsAfterFinished := int32(30) // Delete job 30 seconds after completion

	// Merge annotations from different sources
	mergedAnnotations := make(map[string]string)
	// 1. Add CRD metadata annotations
	for k, v := range clusterImageExport.Annotations {
		mergedAnnotations[k] = v
	}
	// 2. Add controller pod annotations
	for k, v := range r.podAnnotations {
		mergedAnnotations[k] = v
	}
	// 3. Add job-specific annotations from spec (these take precedence)
	for k, v := range clusterImageExport.Spec.JobAnnotations {
		mergedAnnotations[k] = v
	}

	// Set up environment variables for AWS configuration
	envVars := []corev1.EnvVar{}
	if clusterImageExport.Spec.Storage.StorageTarget == shared.STORAGE_S3 {
		if clusterImageExport.Spec.Storage.S3.Region != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "AWS_REGION",
				Value: clusterImageExport.Spec.Storage.S3.Region,
			})
			envVars = append(envVars, corev1.EnvVar{
				Name:  "AWS_DEFAULT_REGION",
				Value: clusterImageExport.Spec.Storage.S3.Region,
			})
		}
	}

	jobParams := shared.JobParams{
		Name:             normalisedImageName,
		Namespace:        clusterImageExport.Namespace,
		Image:            shared.BACKUP_JOB_IMAGE,
		Commands:         defaultCommands,
		Annotations:      mergedAnnotations,
		ServiceAccount:   "",
		ImagePullSecrets: clusterImageExport.Spec.ImagePullSecrets,
		BackoffLimit:     &backoffLimit,
		TTLSecondsAfterFinished: &ttlSecondsAfterFinished,
		EnvVars:         envVars,
	}

	cleanupJob := shared.CreateJob(jobParams, func(raczylocomv1.ClusterImageExport) []string { return nil })

	// Try to create the cleanup job
	if err := r.Create(ctx, cleanupJob); err != nil {
		l.Error(err, "Failed to create cleanup job")
		return err
	}

	l.Info("Created cleanup job with retry limit and TTL")
	return nil
}
