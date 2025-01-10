package shared

import (
	"fmt"
	"os"
	"strings"

	raczylocomv1 "github.com/lukaszraczylo/kubernetes-images-sync-operator/api/raczylo.com/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

type JobParams struct {
	Name                    string
	Namespace               string
	Annotations            map[string]string
	Image                   string
	Commands                []string
	EnvVars                 []corev1.EnvVar
	OwnerReferences         []metav1.OwnerReference
	ServiceAccount          string // Can be empty to use controller's service account
	ImagePullSecrets        []corev1.LocalObjectReference
	BackoffLimit            *int32 // Optional: number of retries before marking the job as failed
	TTLSecondsAfterFinished *int32 // Optional: seconds after which completed/failed job is automatically deleted
}

func CreateJob[T any](params JobParams, setupFunc func(T) []string) *batchv1.Job {
	volumes := []corev1.Volume{}
	volumeMounts := []corev1.VolumeMount{}

	if len(params.ImagePullSecrets) > 0 {
		for i, secret := range params.ImagePullSecrets {
			volumes = append(volumes, corev1.Volume{
				Name: fmt.Sprintf("secret-%d", i),
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: secret.Name,
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      fmt.Sprintf("secret-%d", i),
				MountPath: fmt.Sprintf("/home/runner/.docker-secret-%d", i),
				ReadOnly:  true,
			})
		}
	}

	serviceAccount := params.ServiceAccount
	if serviceAccount == "" {
		serviceAccount = os.Getenv("POD_SERVICE_ACCOUNT")
	}

	j := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            params.Name,
			Namespace:       params.Namespace,
			OwnerReferences: params.OwnerReferences,
			Labels: map[string]string{
				"app": "image-export",
			},
			Annotations: params.Annotations,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            params.BackoffLimit,
			TTLSecondsAfterFinished: params.TTLSecondsAfterFinished,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "image-export",
					},
					Annotations: params.Annotations,
				},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: serviceAccount,
					ImagePullSecrets:   params.ImagePullSecrets,
					Volumes:            volumes,
					Containers: []corev1.Container{
						{
							Name:         "exporter",
							Image:        params.Image,
							TTY:          true,
							Command:      []string{},
							Args:         []string{"/bin/bash", "-c", strings.Join(params.Commands, " && ")},
							VolumeMounts: volumeMounts,
							Env:          params.EnvVars,
							SecurityContext: &corev1.SecurityContext{
								Privileged: pointer.Bool(true),
							},
						},
					},
				},
			},
		},
	}
	return j
}

func SetupS3Params(s3Config raczylocomv1.ClusterImageStorageS3) []string {
	params := []string{}
	if s3Config.UseRole {
		if s3Config.RoleARN != "" {
			// Use specific role
			params = append(params, "--use_role")
			params = append(params, fmt.Sprintf("--role_name='%s'", s3Config.RoleARN))
		} else {
			// Use current role from Kubernetes service account
			params = append(params, "--use_current_role")
		}
	} else {
		params = append(params, fmt.Sprintf("--aws_access_key_id='%s'", s3Config.AccessKey))
		params = append(params, fmt.Sprintf("--aws_secret_access_key='%s'", s3Config.SecretKey))
	}
	if s3Config.Endpoint != "" {
		params = append(params, fmt.Sprintf("--endpoint_url='%s'", s3Config.Endpoint))
	}
	if s3Config.Region != "" {
		params = append(params, fmt.Sprintf("--region=%s", s3Config.Region))
	}
	return params
}
