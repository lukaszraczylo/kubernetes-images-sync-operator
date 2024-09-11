package raczylocom

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	v1batch "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	raczylocomv1 "github.com/lukaszraczylo/kubernetes-images-sync-operator/api/raczylo.com/v1"
	"github.com/lukaszraczylo/kubernetes-images-sync-operator/internal/shared"
)

type ClusterImageReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	MaxParallelJobs int
	ActiveJobs      int
	KubeClient      *kubernetes.Clientset
}

// +kubebuilder:rbac:groups=raczylo.com,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=raczylo.com,resources=*/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=raczylo.com,resources=*/finalizers,verbs=update
// # additional RBAC rules - create and manage jobs
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// add access to secrets
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
func (r *ClusterImageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	clusterImage := &raczylocomv1.ClusterImage{}
	if err := r.Get(ctx, req.NamespacedName, clusterImage); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		l.Error(err, "unable to fetch ClusterImage")
		return ctrl.Result{}, err
	}

	clusterImageExport := &raczylocomv1.ClusterImageExport{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterImage.Spec.ExportName, Namespace: clusterImage.Namespace}, clusterImageExport); err != nil {
		l.Error(err, "unable to fetch ClusterImageExport")
		return ctrl.Result{}, err
	}

	r.MaxParallelJobs = clusterImageExport.Spec.MaxConcurrentJobs

	// If the ClusterImage is new, set its status to PENDING
	if clusterImage.Status.Progress == "" {
		clusterImage.Status.Progress = shared.STATUS_PENDING
		if err := r.Status().Update(ctx, clusterImage); err != nil {
			l.Error(err, "unable to update ClusterImage status to PENDING")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// If we've reached the maximum number of parallel jobs, requeue
	if r.ActiveJobs >= r.MaxParallelJobs && clusterImage.Status.Progress == shared.STATUS_PENDING {
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Process the ClusterImage based on its current status
	switch clusterImage.Status.Progress {
	case shared.STATUS_PENDING:
		return r.handlePendingClusterImage(ctx, clusterImage, l)
	case shared.STATUS_RUNNING, shared.STATUS_RETRYING:
		return r.handleRunningClusterImage(ctx, clusterImage, l)
	case shared.STATUS_SUCCESS, shared.STATUS_FAILED, shared.STATUS_PRESENT:
		return ctrl.Result{}, nil // No further action needed
	default:
		// l.Info("Unexpected ClusterImage status", "Status", clusterImage.Status.Progress)
		return ctrl.Result{}, nil
	}
}

func (r *ClusterImageReconciler) handlePendingClusterImage(ctx context.Context, clusterImage *raczylocomv1.ClusterImage, l logr.Logger) (ctrl.Result, error) {
	// Check if the image is present
	exists, err := r.checkImageExists(ctx, clusterImage)
	if err != nil {
		l.Error(err, "unable to check if image exists")
		return ctrl.Result{}, err
	}
	if exists {
		clusterImage.Status.Progress = shared.STATUS_PRESENT
		if err := r.Status().Update(ctx, clusterImage); err != nil {
			l.Error(err, "unable to update ClusterImage status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Fetch the associated ClusterImageExport
	clusterImageExport := &raczylocomv1.ClusterImageExport{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterImage.Spec.ExportName, Namespace: clusterImage.Namespace}, clusterImageExport); err != nil {
		l.Error(err, "unable to fetch ClusterImageExport")
		return ctrl.Result{}, err
	}

	// Create the backup job
	if err := r.createBackupJob(ctx, clusterImage, clusterImageExport, l); err != nil {
		l.Error(err, "unable to create backup job")
		return ctrl.Result{}, err
	}

	// Update ClusterImage status to RUNNING
	clusterImage.Status.Progress = shared.STATUS_RUNNING
	if err := r.Status().Update(ctx, clusterImage); err != nil {
		l.Error(err, "unable to update ClusterImage status to RUNNING")
		return ctrl.Result{}, err
	}

	// Increment the active jobs count
	r.ActiveJobs++

	return ctrl.Result{Requeue: true}, nil
}

func (r *ClusterImageReconciler) handleRunningClusterImage(ctx context.Context, clusterImage *raczylocomv1.ClusterImage, l logr.Logger) (ctrl.Result, error) {
	// Check for existing job for this ClusterImage
	existingJob := &v1batch.Job{}
	jobName := fmt.Sprintf("img-export-%s", clusterImage.Name)
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: clusterImage.Namespace}, existingJob)

	if err != nil {
		if errors.IsNotFound(err) {
			// Job doesn't exist, set status back to PENDING
			clusterImage.Status.Progress = shared.STATUS_PENDING
			if err := r.Status().Update(ctx, clusterImage); err != nil {
				l.Error(err, "unable to update ClusterImage status back to PENDING")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		l.Error(err, "unable to check for existing job")
		return ctrl.Result{}, err
	}

	// Check job status and update ClusterImage accordingly
	if existingJob.Status.Succeeded > 0 {
		clusterImage.Status.Progress = shared.STATUS_SUCCESS
		r.ActiveJobs--
		if err := r.cleanupJobAndPods(ctx, existingJob); err != nil {
			l.Error(err, "unable to cleanup job and pods")
			return ctrl.Result{}, err
		}
	} else if existingJob.Status.Failed > 0 {
		if clusterImage.Status.RetryCount < 3 {
			clusterImage.Status.Progress = shared.STATUS_RETRYING
			clusterImage.Status.RetryCount++
			if err := r.cleanupJobAndPods(ctx, existingJob); err != nil {
				l.Error(err, "unable to cleanup failed job and pods for retry")
				return ctrl.Result{}, err
			}
			r.ActiveJobs--
			return ctrl.Result{Requeue: true}, nil
		} else {
			clusterImage.Status.Progress = shared.STATUS_FAILED
			r.ActiveJobs--
			if err := r.cleanupJobAndPods(ctx, existingJob); err != nil {
				l.Error(err, "unable to cleanup failed job and pods")
				return ctrl.Result{}, err
			}
		}
	}

	if err := r.handleJobRestarts(ctx, existingJob, clusterImage); err != nil {
		l.Error(err, "unable to handle job restarts")
		return ctrl.Result{}, err
	}

	// Update ClusterImage status
	if err := r.Status().Update(ctx, clusterImage); err != nil {
		l.Error(err, "unable to update ClusterImage status")
		return ctrl.Result{}, err
	}

	return r.updateClusterImageExportStatus(ctx, clusterImage)
}

func (r *ClusterImageReconciler) cleanupJobAndPods(ctx context.Context, job *v1batch.Job) error {
	// Delete the job
	if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete job: %w", err)
	}

	// Delete the associated pods
	labelSelector := metav1.LabelSelector{
		MatchLabels: job.Spec.Selector.MatchLabels,
	}
	listOptions := metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(&labelSelector),
	}

	if err := r.KubeClient.CoreV1().Pods(job.Namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, listOptions); err != nil {
		return fmt.Errorf("failed to delete pods: %w", err)
	}

	return nil
}

func (r *ClusterImageReconciler) createBackupJob(ctx context.Context, clusterImage *raczylocomv1.ClusterImage, clusterImageExport *raczylocomv1.ClusterImageExport, l logr.Logger) error {
	normalisedImageName := shared.NormalizeImageName(clusterImage.Spec.FullName)

	defaultCommands := []string{
		"podman pull " + clusterImage.Spec.FullName,
		"podman save --quiet -o /tmp/" + normalisedImageName + ".tar " + clusterImage.Spec.FullName,
	}

	if clusterImage.Spec.Storage == shared.STORAGE_S3 {
		s3Params := shared.SetupS3Params(clusterImageExport.Spec.Storage.S3)
		additionalCommands := []string{
			"./export.py " + strings.Join(s3Params, " ") + " '/tmp/" + normalisedImageName + ".tar' " + "'s3://" + clusterImageExport.Spec.Storage.S3.Bucket + clusterImage.Spec.ExportPath + "/" + clusterImage.Spec.ExportName + "/" + normalisedImageName + ".tar'",
		}
		defaultCommands = append(defaultCommands, additionalCommands...)
	} else if clusterImage.Spec.Storage == shared.STORAGE_FILE {
		additionalCommands := []string{
			"./export.py /tmp/" + normalisedImageName + ".tar" + " " + clusterImage.Spec.ExportPath + "/" + clusterImage.Spec.ExportName + "/" + normalisedImageName + ".tar",
		}
		defaultCommands = append(defaultCommands, additionalCommands...)
	}
	defaultCommands = append(defaultCommands, "rm -f /tmp/"+normalisedImageName+".tar")

	jobParams := shared.JobParams{
		Name:             fmt.Sprintf("img-export-%s", clusterImage.Name),
		Namespace:        clusterImage.Namespace,
		Image:            shared.BACKUP_JOB_IMAGE,
		Annotations:      clusterImage.Spec.JobAnnotations,
		Commands:         defaultCommands,
		ServiceAccount:   os.Getenv("POD_SERVICE_ACCOUNT"),
		ImagePullSecrets: clusterImage.Spec.ImagePullSecrets,
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion:         clusterImage.APIVersion,
				Kind:               clusterImage.Kind,
				Name:               clusterImage.Name,
				UID:                clusterImage.UID,
				BlockOwnerDeletion: pointer.Bool(true),
				Controller:         pointer.Bool(true),
			},
		},
	}

	backupJob := shared.CreateJob(jobParams, func(raczylocomv1.ClusterImageExport) []string { return nil })

	if err := r.Create(ctx, backupJob); err != nil {
		return err
	}

	clusterImage.Status.Progress = shared.STATUS_RUNNING
	return r.Status().Update(ctx, clusterImage)
}

func (r *ClusterImageReconciler) updateClusterImageExportStatus(ctx context.Context, clusterImage *raczylocomv1.ClusterImage) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	clusterImageExport := &raczylocomv1.ClusterImageExport{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterImage.Spec.ExportName, Namespace: clusterImage.Namespace}, clusterImageExport); err != nil {
		l.Error(err, "unable to fetch ClusterImageExport")
		return ctrl.Result{}, err
	}

	clusterImageList := &raczylocomv1.ClusterImageList{}
	if err := r.List(ctx, clusterImageList, client.InNamespace(clusterImage.Namespace), client.MatchingFields{"spec.exportName": clusterImage.Spec.ExportName}); err != nil {
		l.Error(err, "unable to list ClusterImages")
		return ctrl.Result{}, err
	}

	allCompleted := true
	anyFailed := false
	anyRunning := false

	for _, ci := range clusterImageList.Items {
		switch ci.Status.Progress {
		case shared.STATUS_SUCCESS, shared.STATUS_PRESENT:
			// These statuses are considered completed
		case shared.STATUS_FAILED:
			anyFailed = true
			allCompleted = false
		case shared.STATUS_RUNNING, shared.STATUS_RETRYING:
			allCompleted = false
			anyRunning = true
		case shared.STATUS_PENDING:
			allCompleted = false
		}
	}

	var newStatus string
	if allCompleted {
		newStatus = shared.STATUS_SUCCESS
	} else if anyFailed {
		newStatus = shared.STATUS_FAILED
	} else if anyRunning {
		newStatus = shared.STATUS_RUNNING
	} else {
		newStatus = shared.STATUS_PENDING
	}

	if clusterImageExport.Status.Progress != newStatus {
		clusterImageExport.Status.Progress = newStatus
		if err := r.Status().Update(ctx, clusterImageExport); err != nil {
			l.Error(err, "unable to update ClusterImageExport status")
			return ctrl.Result{}, err
		}
		l.Info("Updated ClusterImageExport status", "ExportName", clusterImageExport.Name, "NewStatus", newStatus)
	}

	// If there are still pending or running images, requeue
	if !allCompleted {
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (r *ClusterImageReconciler) handleJobRestarts(ctx context.Context, job *v1batch.Job, clusterImage *raczylocomv1.ClusterImage) error {
	podList := &v1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(job.Namespace), client.MatchingLabels(job.Spec.Selector.MatchLabels)); err != nil {
		return err
	}

	for _, pod := range podList.Items {
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.RestartCount > 0 {
				clusterImage.Status.RetryCount += int(containerStatus.RestartCount)
				if clusterImage.Status.RetryCount >= 3 {
					clusterImage.Status.Progress = shared.STATUS_FAILED
					if err := r.Status().Update(ctx, clusterImage); err != nil {
						return err
					}
					return r.removeAllJobsAndContainers(ctx, clusterImage.Namespace)
				} else {
					clusterImage.Status.Progress = shared.STATUS_RETRYING
				}

				if err := r.Status().Update(ctx, clusterImage); err != nil {
					return err
				}
				return nil
			}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create a Kubernetes clientset
	var err error
	config := mgr.GetConfig()
	r.KubeClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("unable to create Kubernetes client: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&raczylocomv1.ClusterImage{}).
		Owns(&v1batch.Job{}).
		Complete(r)
}
func (r *ClusterImageReconciler) removeAllJobsAndContainers(ctx context.Context, namespace string) error {
	jobList := &v1batch.JobList{}
	if err := r.List(ctx, jobList, client.InNamespace(namespace), client.MatchingLabels{"app": "image-export"}); err != nil {
		return err
	}

	for _, job := range jobList.Items {
		if err := r.Delete(ctx, &job, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (r *ClusterImageReconciler) checkImageExists(ctx context.Context, clusterImage *raczylocomv1.ClusterImage) (bool, error) {
	clusterImageList := &raczylocomv1.ClusterImageList{}
	if err := r.List(ctx, clusterImageList); err != nil {
		return false, err
	}

	for _, ci := range clusterImageList.Items {
		if ci.Spec.FullName == clusterImage.Spec.FullName && ci.Name != clusterImage.Name {
			if ci.Status.Progress == shared.STATUS_SUCCESS || ci.Status.Progress == shared.STATUS_PRESENT || ci.Status.Progress == shared.STATUS_RUNNING {
				return true, nil
			}
		}
	}

	// Check if the image is already in the COMPLETED state
	if clusterImage.Status.Progress == shared.STATUS_SUCCESS {
		return true, nil
	}

	return false, nil
}

func (r *ClusterImageReconciler) isJobStarted(ctx context.Context, job *v1batch.Job) (bool, error) {
	podList := &v1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(job.Namespace), client.MatchingLabels(job.Spec.Selector.MatchLabels)); err != nil {
		return false, err
	}

	for _, pod := range podList.Items {
		if pod.Status.Phase == v1.PodRunning {
			return true, nil
		}
	}

	return false, nil
}

func (r *ClusterImageReconciler) hasJobTimedOut(job *v1batch.Job) bool {
	// Check if the job has been running for more than 5 minutes without starting
	return time.Since(job.CreationTimestamp.Time) > 5*time.Minute
}
