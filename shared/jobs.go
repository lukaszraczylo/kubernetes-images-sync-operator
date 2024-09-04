package shared

import (
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	raczylocomv1 "raczylo.com/kubernetes-images-sync-operator/api/raczylo.com/v1"
)

type JobParams struct {
	Name            string
	Namespace       string
	Image           string
	Commands        []string
	EnvVars         []corev1.EnvVar
	OwnerReferences []metav1.OwnerReference
}

func CreateJob[T any](params JobParams, setupFunc func(T) []string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            params.Name,
			Namespace:       params.Namespace,
			OwnerReferences: params.OwnerReferences,
			Labels: map[string]string{
				"app": "image-export",
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "image-export",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:  "export",
							Image: params.Image,
							TTY:   true,
							Command: []string{
								"bash",
								"-c",
								strings.Join(params.Commands, " && "),
							},
							Env: params.EnvVars,
							SecurityContext: &corev1.SecurityContext{
								Privileged: pointer.Bool(true),
							},
						},
					},
				},
			},
		},
	}
}

func SetupS3Params(s3Config raczylocomv1.ClusterImageStorageS3) []string {
	params := []string{}
	if s3Config.UseRole {
		params = append(params, "--use-role")
	} else {
		params = append(params, fmt.Sprintf("--aws_access_key_id='%s'", s3Config.AccessKey))
		params = append(params, fmt.Sprintf("--aws_secret_access_key='%s'", s3Config.SecretKey))
	}
	if s3Config.RoleARN != "" {
		params = append(params, fmt.Sprintf("--role_name='%s'", s3Config.RoleARN))
	}
	if s3Config.Endpoint != "" {
		params = append(params, fmt.Sprintf("--endpoint_url='%s'", s3Config.Endpoint))
	}
	if s3Config.Region != "" {
		params = append(params, fmt.Sprintf("--region=%s", s3Config.Region))
	}
	return params
}
