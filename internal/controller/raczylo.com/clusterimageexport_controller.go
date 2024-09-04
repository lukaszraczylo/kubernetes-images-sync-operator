package raczylocom

import (
	"context"
	"crypto/md5"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1batch "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	raczylocomv1 "raczylo.com/kubernetes-images-sync-operator/api/raczylo.com/v1"
	shared "raczylo.com/kubernetes-images-sync-operator/shared"
)

// ClusterImageExportReconciler reconciles a ClusterImageExport object
type ClusterImageExportReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=raczylo.com,resources=clusterimageexports,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=raczylo.com,resources=clusterimageexports/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=raczylo.com,resources=clusterimageexports/finalizers,verbs=update
// additional RBAC rules
// +kubebuilder:rbac:groups=raczylo.com,resources=clusterimages,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch

const clusterImageExportFinalizer = "finalizer.clusterimageexport.raczylo.com"

func (r *ClusterImageExportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	l.Info("Reconciling ClusterImageExport")

	// Fetch the ClusterImageExport instance
	clusterImageExport := &raczylocomv1.ClusterImageExport{}
	if err := r.Get(ctx, req.NamespacedName, clusterImageExport); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !clusterImageExport.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, clusterImageExport)
	}

	// Add finalizer if it doesn't exist
	if !controllerutil.ContainsFinalizer(clusterImageExport, clusterImageExportFinalizer) {
		controllerutil.AddFinalizer(clusterImageExport, clusterImageExportFinalizer)
		if err := r.Update(ctx, clusterImageExport); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Early return if the ClusterImageExport is already in a completed state
	if clusterImageExport.Status.Progress == shared.STATUS_SUCCESS || clusterImageExport.Status.Progress == shared.STATUS_FAILED {
		l.Info("ClusterImageExport is already in a completed state", "Status", clusterImageExport.Status.Progress)
		return ctrl.Result{}, nil
	}

	// If the status is empty, set it to PENDING
	if clusterImageExport.Status.Progress == "" {
		clusterImageExport.Status.Progress = shared.STATUS_PENDING
		if err := r.Status().Update(ctx, clusterImageExport); err != nil {
			l.Error(err, "unable to update ClusterImageExport status")
			return ctrl.Result{}, err
		}
	}

	// Proceed with the rest of the reconciliation logic
	fullImagesList, err := r.listImagesInCluster(ctx, l, clusterImageExport)
	if err != nil {
		l.Error(err, "unable to list images in the cluster")
		return ctrl.Result{}, err
	}

	clusterImageExport.Status.Progress = shared.STATUS_RUNNING
	if err := r.Status().Update(ctx, clusterImageExport); err != nil {
		l.Error(err, "unable to update ClusterImageExport status to RUNNING")
		return ctrl.Result{}, err
	}

	for _, image := range fullImagesList.Containers {
		nameHash := fmt.Sprintf("%x", md5.Sum([]byte(clusterImageExport.Name+image.Image+image.Tag+image.Sha)))[:14]

		// Check if the ClusterImage already exists
		clusterImage := &raczylocomv1.ClusterImage{}
		err := r.Get(ctx, client.ObjectKey{Namespace: clusterImageExport.Namespace, Name: nameHash}, clusterImage)
		if err == nil {
			// ClusterImage exists, check its status
			if clusterImage.Status.Progress == shared.STATUS_FAILED {
				clusterImageExport.Status.Progress = shared.STATUS_FAILED
				if err := r.Status().Update(ctx, clusterImageExport); err != nil {
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
				Image:      image.Image,
				Tag:        image.Tag,
				Sha:        image.Sha,
				FullName:   image.FullName,
				Storage:    clusterImageExport.Spec.Storage.StorageTarget,
				ExportName: clusterImageExport.Name,
				ExportPath: clusterImageExport.Spec.BasePath,
			},
		}

		if err := r.Create(ctx, newClusterImage); err != nil {
			l.Error(err, "unable to create ClusterImage", "image", image)
			return ctrl.Result{}, err
		}
	}

	// Check if all ClusterImages are completed
	allCompleted, err := r.checkAllClusterImagesCompleted(ctx, clusterImageExport)
	if err != nil {
		l.Error(err, "unable to check ClusterImages status")
		return ctrl.Result{}, err
	}

	if allCompleted {
		clusterImageExport.Status.Progress = shared.STATUS_SUCCESS
		if err := r.Status().Update(ctx, clusterImageExport); err != nil {
			l.Error(err, "unable to update ClusterImageExport status to SUCCESS")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{Requeue: !allCompleted}, nil
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

	containersList = shared.RemoveDuplicates(containersList)
	l.Info("List of containers in the cluster", "containers", containersList)

	return containersList, nil
}

func (r *ClusterImageExportReconciler) handleDeletion(ctx context.Context, clusterImageExport *raczylocomv1.ClusterImageExport) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(clusterImageExport, clusterImageExportFinalizer) {
		// Run the cleanup job
		if err := r.runCleanupJob(ctx, clusterImageExport); err != nil {
			l.Error(err, "Failed to run cleanup job")
			return ctrl.Result{}, err
		}

		// Remove the finalizer
		controllerutil.RemoveFinalizer(clusterImageExport, clusterImageExportFinalizer)
		if err := r.Update(ctx, clusterImageExport); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
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

	jobParams := shared.JobParams{
		Name:      normalisedImageName,
		Namespace: clusterImageExport.Namespace,
		Image:     shared.BACKUP_JOB_IMAGE,
		Commands:  defaultCommands,
	}

	cleanupJob := shared.CreateJob(jobParams, func(raczylocomv1.ClusterImageExport) []string { return nil })

	if err := r.Create(ctx, cleanupJob); err != nil {
		l.Error(err, "Failed to create cleanup job")
		return err
	}

	l.Info("Created cleanup job")

	go func() {
		if err := r.waitForJobCompletionAndDelete(ctx, cleanupJob); err != nil {
			l.Error(err, "Failed to wait for job completion and delete")
		}
	}()
	return nil
}

func (r *ClusterImageExportReconciler) waitForJobCompletionAndDelete(ctx context.Context, job *v1batch.Job) error {
	l := log.FromContext(ctx)
	key := client.ObjectKeyFromObject(job)

	// Wait for the job to complete
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := r.Get(ctx, key, job); err != nil {
				return err
			}

			if job.Status.Succeeded > 0 {
				// Job completed successfully, delete it
				l.Info("Cleanup job completed, deleting", "job", job.Name)
				if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
					return err
				}
				return nil
			}

			if job.Status.Failed > 0 {
				// Job failed, log the error but still delete the job
				l.Error(nil, "Cleanup job failed", "job", job.Name)
				if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
					return err
				}
				return fmt.Errorf("cleanup job failed: %s", job.Name)
			}

			// Job still running, wait and check again
			time.Sleep(5 * time.Second)
		}
	}
}
