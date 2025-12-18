package shared

import (
	"testing"

	raczylocomv1 "github.com/lukaszraczylo/kubernetes-images-sync-operator/api/raczylo.com/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// JobsTestSuite tests job creation and related functionality
type JobsTestSuite struct {
	suite.Suite
}

func TestJobsTestSuite(t *testing.T) {
	suite.Run(t, new(JobsTestSuite))
}

// TestCreateJob tests the CreateJob function with various scenarios
func (s *JobsTestSuite) TestCreateJob() {
	testCases := []struct {
		name              string
		scenario          TestScenario
		params            JobParams
		expectJobName     string
		expectNamespace   string
		expectImage       string
		expectAnnotations map[string]string
		expectSecrets     int
		expectEnvVars     int
	}{
		// Good scenarios
		{
			name:     "basic job creation",
			scenario: ScenarioGood,
			params: JobParams{
				Name:      "test-job",
				Namespace: "default",
				Image:     "worker:latest",
				Commands:  []string{"echo hello"},
			},
			expectJobName:   "test-job",
			expectNamespace: "default",
			expectImage:     "worker:latest",
			expectSecrets:   0,
			expectEnvVars:   0,
		},
		{
			name:     "job with annotations",
			scenario: ScenarioGood,
			params: JobParams{
				Name:      "annotated-job",
				Namespace: "default",
				Image:     "worker:latest",
				Commands:  []string{"echo hello"},
				Annotations: map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			},
			expectJobName:   "annotated-job",
			expectNamespace: "default",
			expectImage:     "worker:latest",
			expectAnnotations: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			expectSecrets: 0,
			expectEnvVars: 0,
		},
		{
			name:     "job with image pull secrets",
			scenario: ScenarioGood,
			params: JobParams{
				Name:      "secret-job",
				Namespace: "default",
				Image:     "private-registry/image:latest",
				Commands:  []string{"echo hello"},
				ImagePullSecrets: []corev1.LocalObjectReference{
					{Name: "my-registry-secret"},
				},
			},
			expectJobName:   "secret-job",
			expectNamespace: "default",
			expectImage:     "private-registry/image:latest",
			expectSecrets:   1,
			expectEnvVars:   0,
		},
		{
			name:     "job with multiple secrets",
			scenario: ScenarioGood,
			params: JobParams{
				Name:      "multi-secret-job",
				Namespace: "default",
				Image:     "private-registry/image:latest",
				Commands:  []string{"echo hello"},
				ImagePullSecrets: []corev1.LocalObjectReference{
					{Name: "secret1"},
					{Name: "secret2"},
					{Name: "secret3"},
				},
			},
			expectJobName:   "multi-secret-job",
			expectNamespace: "default",
			expectImage:     "private-registry/image:latest",
			expectSecrets:   3,
			expectEnvVars:   0,
		},
		{
			name:     "job with environment variables",
			scenario: ScenarioGood,
			params: JobParams{
				Name:      "env-job",
				Namespace: "default",
				Image:     "worker:latest",
				Commands:  []string{"echo $MY_VAR"},
				EnvVars: []corev1.EnvVar{
					{Name: "MY_VAR", Value: "my-value"},
					{Name: "AWS_REGION", Value: "us-east-1"},
				},
			},
			expectJobName:   "env-job",
			expectNamespace: "default",
			expectImage:     "worker:latest",
			expectSecrets:   0,
			expectEnvVars:   2,
		},

		// Not good scenarios
		{
			name:     "job with backoff limit",
			scenario: ScenarioNotGood,
			params: JobParams{
				Name:         "retry-job",
				Namespace:    "default",
				Image:        "worker:latest",
				Commands:     []string{"might-fail"},
				BackoffLimit: int32Ptr(3),
			},
			expectJobName:   "retry-job",
			expectNamespace: "default",
			expectImage:     "worker:latest",
			expectSecrets:   0,
			expectEnvVars:   0,
		},
		{
			name:     "job with TTL after finished",
			scenario: ScenarioNotGood,
			params: JobParams{
				Name:                    "ttl-job",
				Namespace:               "default",
				Image:                   "worker:latest",
				Commands:                []string{"echo done"},
				TTLSecondsAfterFinished: int32Ptr(300),
			},
			expectJobName:   "ttl-job",
			expectNamespace: "default",
			expectImage:     "worker:latest",
			expectSecrets:   0,
			expectEnvVars:   0,
		},

		// Really bad scenarios - edge cases
		{
			name:     "job with empty commands",
			scenario: ScenarioReallyBad,
			params: JobParams{
				Name:      "empty-cmd-job",
				Namespace: "default",
				Image:     "worker:latest",
				Commands:  []string{},
			},
			expectJobName:   "empty-cmd-job",
			expectNamespace: "default",
			expectImage:     "worker:latest",
			expectSecrets:   0,
			expectEnvVars:   0,
		},
		{
			name:     "job with very long name",
			scenario: ScenarioReallyBad,
			params: JobParams{
				Name:      "this-is-a-very-long-job-name-that-might-cause-issues-in-kubernetes-because-names-have-limits",
				Namespace: "default",
				Image:     "worker:latest",
				Commands:  []string{"echo hello"},
			},
			expectJobName:   "this-is-a-very-long-job-name-that-might-cause-issues-in-kubernetes-because-names-have-limits",
			expectNamespace: "default",
			expectImage:     "worker:latest",
			expectSecrets:   0,
			expectEnvVars:   0,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			job := CreateJob(tc.params, func(raczylocomv1.ClusterImageExport) []string { return nil })

			require.NotNil(s.T(), job, "Job should not be nil")

			// Verify job metadata
			assert.Equal(s.T(), tc.expectJobName, job.ObjectMeta.Name)
			assert.Equal(s.T(), tc.expectNamespace, job.ObjectMeta.Namespace)

			// Verify labels
			assert.Equal(s.T(), "image-export", job.ObjectMeta.Labels["app"])
			assert.Equal(s.T(), "image-export", job.Spec.Template.ObjectMeta.Labels["app"])

			// Verify annotations if expected
			if tc.expectAnnotations != nil {
				for k, v := range tc.expectAnnotations {
					assert.Equal(s.T(), v, job.ObjectMeta.Annotations[k])
					assert.Equal(s.T(), v, job.Spec.Template.ObjectMeta.Annotations[k])
				}
			}

			// Verify pod template
			podSpec := job.Spec.Template.Spec
			require.Len(s.T(), podSpec.Containers, 1, "Should have exactly one container")

			container := podSpec.Containers[0]
			assert.Equal(s.T(), "exporter", container.Name)
			assert.Equal(s.T(), tc.expectImage, container.Image)
			assert.True(s.T(), container.TTY)

			// Verify restart policy
			assert.Equal(s.T(), corev1.RestartPolicyOnFailure, podSpec.RestartPolicy)

			// Verify secrets
			assert.Len(s.T(), podSpec.ImagePullSecrets, tc.expectSecrets)
			assert.Len(s.T(), podSpec.Volumes, tc.expectSecrets)
			assert.Len(s.T(), container.VolumeMounts, tc.expectSecrets)

			// Verify environment variables
			assert.Len(s.T(), container.Env, tc.expectEnvVars)

			// Verify security context (privileged for podman)
			require.NotNil(s.T(), container.SecurityContext)
			require.NotNil(s.T(), container.SecurityContext.Privileged)
			assert.True(s.T(), *container.SecurityContext.Privileged)
		})
	}
}

// TestCreateJobWithOwnerReferences tests owner reference handling
func (s *JobsTestSuite) TestCreateJobWithOwnerReferences() {
	ownerRefs := []metav1.OwnerReference{
		{
			APIVersion: "raczylo.com/v1",
			Kind:       "ClusterImage",
			Name:       "test-image",
			UID:        "test-uid-12345",
		},
	}

	params := JobParams{
		Name:            "owned-job",
		Namespace:       "default",
		Image:           "worker:latest",
		Commands:        []string{"echo hello"},
		OwnerReferences: ownerRefs,
	}

	job := CreateJob(params, func(raczylocomv1.ClusterImageExport) []string { return nil })

	require.Len(s.T(), job.ObjectMeta.OwnerReferences, 1)
	assert.Equal(s.T(), "ClusterImage", job.ObjectMeta.OwnerReferences[0].Kind)
	assert.Equal(s.T(), "test-image", job.ObjectMeta.OwnerReferences[0].Name)
}

// TestCreateJobCommands tests command concatenation
func (s *JobsTestSuite) TestCreateJobCommands() {
	testCases := []struct {
		name            string
		commands        []string
		expectedArgsLen int
		expectedJoined  string
	}{
		{
			name:            "single command",
			commands:        []string{"echo hello"},
			expectedArgsLen: 3, // /bin/bash, -c, "command"
			expectedJoined:  "echo hello",
		},
		{
			name:            "multiple commands",
			commands:        []string{"echo hello", "echo world"},
			expectedArgsLen: 3,
			expectedJoined:  "echo hello && echo world",
		},
		{
			name: "complex podman commands",
			commands: []string{
				"podman pull nginx:latest",
				"podman save --quiet -o /tmp/nginx.tar nginx:latest",
				"./worker export /tmp/nginx.tar s3://bucket/path",
			},
			expectedArgsLen: 3,
			expectedJoined:  "podman pull nginx:latest && podman save --quiet -o /tmp/nginx.tar nginx:latest && ./worker export /tmp/nginx.tar s3://bucket/path",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			params := JobParams{
				Name:      "cmd-test",
				Namespace: "default",
				Image:     "worker:latest",
				Commands:  tc.commands,
			}

			job := CreateJob(params, func(raczylocomv1.ClusterImageExport) []string { return nil })

			container := job.Spec.Template.Spec.Containers[0]
			assert.Len(s.T(), container.Args, tc.expectedArgsLen)
			assert.Equal(s.T(), "/bin/bash", container.Args[0])
			assert.Equal(s.T(), "-c", container.Args[1])
			assert.Equal(s.T(), tc.expectedJoined, container.Args[2])
		})
	}
}

// TestCreateJobBackoffAndTTL tests backoff limit and TTL settings
func (s *JobsTestSuite) TestCreateJobBackoffAndTTL() {
	backoff := int32(5)
	ttl := int32(600)

	params := JobParams{
		Name:                    "backoff-ttl-job",
		Namespace:               "default",
		Image:                   "worker:latest",
		Commands:                []string{"echo hello"},
		BackoffLimit:            &backoff,
		TTLSecondsAfterFinished: &ttl,
	}

	job := CreateJob(params, func(raczylocomv1.ClusterImageExport) []string { return nil })

	require.NotNil(s.T(), job.Spec.BackoffLimit)
	assert.Equal(s.T(), int32(5), *job.Spec.BackoffLimit)

	require.NotNil(s.T(), job.Spec.TTLSecondsAfterFinished)
	assert.Equal(s.T(), int32(600), *job.Spec.TTLSecondsAfterFinished)
}

// TestSetupS3Params tests S3 parameter generation
func (s *JobsTestSuite) TestSetupS3Params() {
	testCases := []struct {
		name           string
		scenario       TestScenario
		config         raczylocomv1.ClusterImageStorageS3
		expectContains []string
		expectNotIn    []string
	}{
		// Good scenarios
		{
			name:     "basic credentials",
			scenario: ScenarioGood,
			config: raczylocomv1.ClusterImageStorageS3{
				Bucket:    "my-bucket",
				Region:    "us-east-1",
				AccessKey: "AKIAIOSFODNN7EXAMPLE",
				SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			},
			expectContains: []string{
				"--aws_access_key_id='AKIAIOSFODNN7EXAMPLE'",
				"--aws_secret_access_key='wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY'",
			},
			expectNotIn: []string{"--use_role", "--use_current_role"},
		},
		{
			name:     "use current role (IRSA)",
			scenario: ScenarioGood,
			config: raczylocomv1.ClusterImageStorageS3{
				Bucket:  "my-bucket",
				Region:  "us-east-1",
				UseRole: true,
			},
			expectContains: []string{"--use_current_role"},
			expectNotIn:    []string{"--aws_access_key_id", "--aws_secret_access_key"},
		},
		{
			name:     "use specific role ARN",
			scenario: ScenarioGood,
			config: raczylocomv1.ClusterImageStorageS3{
				Bucket:  "my-bucket",
				Region:  "us-east-1",
				UseRole: true,
				RoleARN: "arn:aws:iam::123456789:role/MyRole",
			},
			expectContains: []string{
				"--use_role",
				"--role_name='arn:aws:iam::123456789:role/MyRole'",
			},
			expectNotIn: []string{"--aws_access_key_id", "--use_current_role"},
		},

		// Not good scenarios
		{
			name:     "with custom endpoint (MinIO)",
			scenario: ScenarioNotGood,
			config: raczylocomv1.ClusterImageStorageS3{
				Bucket:    "my-bucket",
				Region:    "us-east-1",
				AccessKey: "minioadmin",
				SecretKey: "minioadmin",
				Endpoint:  "http://minio.local:9000",
			},
			expectContains: []string{
				"--endpoint_url='http://minio.local:9000'",
				"--aws_access_key_id='minioadmin'",
			},
		},

		// Really bad scenarios
		{
			name:     "empty credentials with UseRole false",
			scenario: ScenarioReallyBad,
			config: raczylocomv1.ClusterImageStorageS3{
				Bucket: "my-bucket",
				Region: "us-east-1",
			},
			expectContains: []string{
				"--aws_access_key_id=''",
				"--aws_secret_access_key=''",
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			params := SetupS3Params(tc.config)

			// Check expected parameters are present
			for _, expected := range tc.expectContains {
				found := false
				for _, param := range params {
					if param == expected {
						found = true
						break
					}
				}
				assert.True(s.T(), found, "Expected parameter not found: %s", expected)
			}

			// Check unexpected parameters are not present
			for _, notExpected := range tc.expectNotIn {
				for _, param := range params {
					assert.NotContains(s.T(), param, notExpected, "Unexpected parameter found: %s", notExpected)
				}
			}
		})
	}
}

// TestJobParamsDefaults tests JobParams default handling
func (s *JobsTestSuite) TestJobParamsDefaults() {
	s.Run("nil backoff limit", func() {
		params := JobParams{
			Name:      "test",
			Namespace: "default",
			Image:     "worker:latest",
			Commands:  []string{"echo"},
		}

		job := CreateJob(params, func(raczylocomv1.ClusterImageExport) []string { return nil })
		assert.Nil(s.T(), job.Spec.BackoffLimit)
	})

	s.Run("nil TTL", func() {
		params := JobParams{
			Name:      "test",
			Namespace: "default",
			Image:     "worker:latest",
			Commands:  []string{"echo"},
		}

		job := CreateJob(params, func(raczylocomv1.ClusterImageExport) []string { return nil })
		assert.Nil(s.T(), job.Spec.TTLSecondsAfterFinished)
	})

	s.Run("empty service account uses env var", func() {
		// This test verifies that when ServiceAccount is empty,
		// the job will use POD_SERVICE_ACCOUNT env var
		params := JobParams{
			Name:           "test",
			Namespace:      "default",
			Image:          "worker:latest",
			Commands:       []string{"echo"},
			ServiceAccount: "", // Empty
		}

		job := CreateJob(params, func(raczylocomv1.ClusterImageExport) []string { return nil })
		// Service account will be set from environment variable POD_SERVICE_ACCOUNT
		// In tests, this will be empty, so we just verify the job is created
		require.NotNil(s.T(), job)
	})
}

// TestSecretVolumeMounting tests that secrets are properly mounted
func (s *JobsTestSuite) TestSecretVolumeMounting() {
	secrets := []corev1.LocalObjectReference{
		{Name: "docker-registry"},
		{Name: "gcr-json-key"},
		{Name: "ecr-credentials"},
	}

	params := JobParams{
		Name:             "secret-mount-test",
		Namespace:        "default",
		Image:            "worker:latest",
		Commands:         []string{"echo"},
		ImagePullSecrets: secrets,
	}

	job := CreateJob(params, func(raczylocomv1.ClusterImageExport) []string { return nil })
	podSpec := job.Spec.Template.Spec
	container := podSpec.Containers[0]

	// Verify volumes are created
	require.Len(s.T(), podSpec.Volumes, 3)
	for i, vol := range podSpec.Volumes {
		assert.Equal(s.T(), secrets[i].Name, vol.VolumeSource.Secret.SecretName)
		assert.Contains(s.T(), vol.Name, "secret-")
	}

	// Verify volume mounts
	require.Len(s.T(), container.VolumeMounts, 3)
	for i, mount := range container.VolumeMounts {
		assert.Contains(s.T(), mount.MountPath, ".docker-secret-")
		assert.True(s.T(), mount.ReadOnly)
		assert.Contains(s.T(), mount.Name, "secret-")
		// Verify index is correct
		expectedIndex := i
		assert.Equal(s.T(), "/home/runner/.docker-secret-"+string(rune('0'+expectedIndex)), mount.MountPath)
	}
}

// Helper function
func int32Ptr(i int32) *int32 {
	return &i
}
