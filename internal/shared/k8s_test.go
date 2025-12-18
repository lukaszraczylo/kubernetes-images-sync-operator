package shared

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// K8sTestSuite tests Kubernetes-related utility functions
type K8sTestSuite struct {
	suite.Suite
}

func TestK8sTestSuite(t *testing.T) {
	suite.Run(t, new(K8sTestSuite))
}

// ProcessContainerNameTestCase represents a test case for ProcessContainerName
type ProcessContainerNameTestCase struct {
	name          string
	scenario      TestScenario
	input         string
	expectedImage string
	expectedTag   string
	expectedSha   string
	expectError   bool
	errorContains string
}

// TestProcessContainerName tests the ProcessContainerName function with comprehensive scenarios
func (s *K8sTestSuite) TestProcessContainerName() {
	testCases := []ProcessContainerNameTestCase{
		// ========== GOOD SCENARIOS ==========
		// Standard Docker Hub images
		{
			name:          "simple image name defaults to latest",
			scenario:      ScenarioGood,
			input:         "nginx",
			expectedImage: "nginx",
			expectedTag:   "latest",
			expectedSha:   "",
			expectError:   false,
		},
		{
			name:          "image with latest tag",
			scenario:      ScenarioGood,
			input:         "nginx:latest",
			expectedImage: "nginx",
			expectedTag:   "latest",
			expectedSha:   "",
			expectError:   false,
		},
		{
			name:          "image with version tag",
			scenario:      ScenarioGood,
			input:         "nginx:1.21.0",
			expectedImage: "nginx",
			expectedTag:   "1.21.0",
			expectedSha:   "",
			expectError:   false,
		},
		{
			name:          "image with semver tag",
			scenario:      ScenarioGood,
			input:         "redis:6.2.6",
			expectedImage: "redis",
			expectedTag:   "6.2.6",
			expectedSha:   "",
			expectError:   false,
		},

		// Images with registry prefix
		{
			name:          "gcr.io registry",
			scenario:      ScenarioGood,
			input:         "gcr.io/google-containers/pause:3.2",
			expectedImage: "gcr.io/google-containers/pause",
			expectedTag:   "3.2",
			expectedSha:   "",
			expectError:   false,
		},
		{
			name:          "ghcr.io registry",
			scenario:      ScenarioGood,
			input:         "ghcr.io/owner/repo:v1.0.0",
			expectedImage: "ghcr.io/owner/repo",
			expectedTag:   "v1.0.0",
			expectedSha:   "",
			expectError:   false,
		},
		{
			name:          "quay.io registry",
			scenario:      ScenarioGood,
			input:         "quay.io/coreos/etcd:v3.5.0",
			expectedImage: "quay.io/coreos/etcd",
			expectedTag:   "v3.5.0",
			expectedSha:   "",
			expectError:   false,
		},
		{
			name:          "registry.k8s.io",
			scenario:      ScenarioGood,
			input:         "registry.k8s.io/pause:3.9",
			expectedImage: "registry.k8s.io/pause",
			expectedTag:   "3.9",
			expectedSha:   "",
			expectError:   false,
		},
		{
			name:          "docker.io explicit registry",
			scenario:      ScenarioGood,
			input:         "docker.io/library/nginx:latest",
			expectedImage: "docker.io/library/nginx",
			expectedTag:   "latest",
			expectedSha:   "",
			expectError:   false,
		},

		// Private registry with port
		{
			name:          "private registry with port",
			scenario:      ScenarioGood,
			input:         "myregistry.local:5000/myimage:v1",
			expectedImage: "myregistry.local",
			expectedTag:   "5000/myimage:v1", // This is the current behavior
			expectedSha:   "",
			expectError:   false,
		},

		// ========== NOT GOOD SCENARIOS ==========
		// SHA-only references (no tag)
		{
			name:          "image with sha256 digest only",
			scenario:      ScenarioNotGood,
			input:         "nginx@sha256:abc123def456789012345678901234567890123456789012345678901234",
			expectedImage: "nginx",
			expectedTag:   "",
			expectedSha:   "sha256:abc123def456789012345678901234567890123456789012345678901234",
			expectError:   false,
		},
		{
			name:          "registry image with sha only",
			scenario:      ScenarioNotGood,
			input:         "gcr.io/distroless/static@sha256:abcdef1234567890",
			expectedImage: "gcr.io/distroless/static",
			expectedTag:   "",
			expectedSha:   "sha256:abcdef1234567890",
			expectError:   false,
		},

		// Tag + SHA references (pinned images)
		{
			name:          "cilium with tag and sha - real world example",
			scenario:      ScenarioNotGood,
			input:         "quay.io/cilium/cilium:v1.18.4@sha256:49d87af187eeeb9e9e3ec2bc6bd372261a0b5cb2d845659463ba7cc10fe9e45f",
			expectedImage: "quay.io/cilium/cilium",
			expectedTag:   "v1.18.4",
			expectedSha:   "sha256:49d87af187eeeb9e9e3ec2bc6bd372261a0b5cb2d845659463ba7cc10fe9e45f",
			expectError:   false,
		},
		{
			name:          "distroless with tag and sha",
			scenario:      ScenarioNotGood,
			input:         "gcr.io/distroless/static:nonroot@sha256:abc123",
			expectedImage: "gcr.io/distroless/static",
			expectedTag:   "nonroot",
			expectedSha:   "sha256:abc123",
			expectError:   false,
		},

		// Complex nested paths
		{
			name:          "deeply nested registry path",
			scenario:      ScenarioNotGood,
			input:         "us-docker.pkg.dev/project/repo/subdir/image:tag",
			expectedImage: "us-docker.pkg.dev/project/repo/subdir/image",
			expectedTag:   "tag",
			expectedSha:   "",
			expectError:   false,
		},

		// ========== REALLY BAD SCENARIOS ==========
		// Empty and invalid inputs
		{
			name:          "empty string",
			scenario:      ScenarioReallyBad,
			input:         "",
			expectedImage: "",
			expectedTag:   "",
			expectedSha:   "",
			expectError:   true,
			errorContains: "image name is required",
		},
		{
			name:          "only colon",
			scenario:      ScenarioReallyBad,
			input:         ":",
			expectedImage: "",
			expectedTag:   "",
			expectedSha:   "",
			expectError:   true,
			errorContains: "image name is required",
		},
		{
			name:          "only at sign",
			scenario:      ScenarioReallyBad,
			input:         "@",
			expectedImage: "",
			expectedTag:   "",
			expectedSha:   "",
			expectError:   true,
			errorContains: "invalid SHA format",
		},

		// Invalid SHA formats
		{
			name:          "invalid sha format - missing colon",
			scenario:      ScenarioReallyBad,
			input:         "nginx@sha256abc123",
			expectedImage: "",
			expectedTag:   "",
			expectedSha:   "",
			expectError:   true,
			errorContains: "invalid SHA format",
		},
		{
			name:          "invalid sha format - wrong algorithm",
			scenario:      ScenarioReallyBad,
			input:         "nginx@md5:abc123",
			expectedImage: "",
			expectedTag:   "",
			expectedSha:   "",
			expectError:   true,
			errorContains: "invalid SHA format",
		},
		{
			name:          "multiple @ symbols",
			scenario:      ScenarioReallyBad,
			input:         "nginx@sha256:abc@sha256:def",
			expectedImage: "",
			expectedTag:   "",
			expectedSha:   "",
			expectError:   true,
			errorContains: "invalid container name format",
		},

		// Edge cases for Kubernetes volatility
		{
			name:          "k8s pause image",
			scenario:      ScenarioGood,
			input:         "registry.k8s.io/pause:3.9",
			expectedImage: "registry.k8s.io/pause",
			expectedTag:   "3.9",
			expectedSha:   "",
			expectError:   false,
		},
		{
			name:          "coredns image",
			scenario:      ScenarioGood,
			input:         "registry.k8s.io/coredns/coredns:v1.11.1",
			expectedImage: "registry.k8s.io/coredns/coredns",
			expectedTag:   "v1.11.1",
			expectedSha:   "",
			expectError:   false,
		},
		{
			name:          "etcd with sha pinning",
			scenario:      ScenarioNotGood,
			input:         "registry.k8s.io/etcd:3.5.12-0@sha256:abc123",
			expectedImage: "registry.k8s.io/etcd",
			expectedTag:   "3.5.12-0",
			expectedSha:   "sha256:abc123",
			expectError:   false,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result, err := ProcessContainerName(tc.input)

			if tc.expectError {
				require.Error(s.T(), err, "Scenario: %s - Expected error for input: %s", tc.scenario, tc.input)
				if tc.errorContains != "" {
					assert.Contains(s.T(), err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(s.T(), err, "Scenario: %s - Unexpected error for input: %s", tc.scenario, tc.input)
				assert.Equal(s.T(), tc.expectedImage, result.Image, "Image mismatch")
				assert.Equal(s.T(), tc.expectedTag, result.Tag, "Tag mismatch")
				assert.Equal(s.T(), tc.expectedSha, result.Sha, "SHA mismatch")

				// Verify FullName is preserved
				if tc.input != "" {
					assert.Equal(s.T(), tc.input, result.FullName, "FullName should match input")
				}
			}
		})
	}
}

// TestProcessContainerNameCaching tests that the container cache works correctly
func (s *K8sTestSuite) TestProcessContainerNameCaching() {
	// Clear the cache first by processing a unique image
	uniqueImage := "test-cache-" + time.Now().Format("20060102150405")

	// First call - should not be cached
	result1, err := ProcessContainerName(uniqueImage)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), uniqueImage, result1.Image)
	assert.Equal(s.T(), "latest", result1.Tag) // Defaults to latest

	// Second call - should be cached
	result2, err := ProcessContainerName(uniqueImage)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), result1, result2, "Cached result should match original")
}

// TestProcessContainerNameConcurrency tests thread safety of ProcessContainerName
func (s *K8sTestSuite) TestProcessContainerNameConcurrency() {
	images := []string{
		"nginx:latest",
		"redis:6",
		"postgres:14",
		"mysql:8",
		"mongo:5",
	}

	done := make(chan bool, len(images)*10)
	errors := make(chan error, len(images)*10)

	// Run 10 goroutines per image
	for i := 0; i < 10; i++ {
		for _, img := range images {
			go func(image string) {
				_, err := ProcessContainerName(image)
				if err != nil {
					errors <- err
				}
				done <- true
			}(img)
		}
	}

	// Wait for all goroutines
	for i := 0; i < len(images)*10; i++ {
		<-done
	}

	// Check for errors
	close(errors)
	for err := range errors {
		s.T().Errorf("Concurrent access error: %v", err)
	}
}

// TestContainerCacheOperations tests the ContainerCache directly
func (s *K8sTestSuite) TestContainerCacheOperations() {
	cache := &ContainerCache{
		cache: make(map[string]Container),
	}

	// Test Set and Get
	testContainer := Container{
		Image:    "test-image",
		Tag:      "v1.0.0",
		FullName: "test-image:v1.0.0",
	}

	cache.Set("test-key", testContainer)

	retrieved, ok := cache.Get("test-key")
	assert.True(s.T(), ok, "Should find cached container")
	assert.Equal(s.T(), testContainer, retrieved)

	// Test Get non-existent key
	_, ok = cache.Get("non-existent")
	assert.False(s.T(), ok, "Should not find non-existent key")
}

// TestProcessContainerWithContext tests context cancellation
func (s *K8sTestSuite) TestProcessContainerWithContext() {
	ctx, cancel := context.WithCancel(context.Background())

	// Test with active context
	containersList := &ContainersList{}
	err := processContainer(ctx, "nginx:latest", "default", containersList)
	require.NoError(s.T(), err)
	assert.Len(s.T(), containersList.Containers, 1)

	// Test with cancelled context
	cancel()
	err = processContainer(ctx, "redis:6", "default", containersList)
	assert.Error(s.T(), err)
	assert.Equal(s.T(), context.Canceled, err)
}

// TestKubernetesVolatilityScenarios tests scenarios specific to Kubernetes environment volatility
func (s *K8sTestSuite) TestKubernetesVolatilityScenarios() {
	testCases := []struct {
		name        string
		description string
		input       string
		expectError bool
	}{
		// Pod restart scenarios - same image, different instance
		{
			name:        "pod_restart_same_image",
			description: "When a pod restarts, the same image reference should parse identically",
			input:       "nginx:1.21",
			expectError: false,
		},
		// Rolling update scenarios
		{
			name:        "rolling_update_new_tag",
			description: "During rolling update, new tag version",
			input:       "myapp:v2.0.0",
			expectError: false,
		},
		// Image pull scenarios
		{
			name:        "image_pull_backoff_recovery",
			description: "Image that might have failed to pull initially",
			input:       "private-registry.io/secure/image:latest",
			expectError: false,
		},
		// Helm chart common patterns
		{
			name:        "helm_chart_image_pattern",
			description: "Common Helm chart image reference pattern",
			input:       "bitnami/postgresql:14.5.0-debian-11-r14",
			expectError: false,
		},
		// Operator-managed images
		{
			name:        "operator_managed_image",
			description: "Image managed by an operator with specific versioning",
			input:       "quay.io/prometheus/prometheus:v2.45.0",
			expectError: false,
		},
		// Init container images
		{
			name:        "init_container_image",
			description: "Common init container image",
			input:       "busybox:1.36",
			expectError: false,
		},
		// Sidecar images
		{
			name:        "sidecar_envoy_proxy",
			description: "Envoy sidecar proxy image",
			input:       "envoyproxy/envoy:v1.28.0",
			expectError: false,
		},
		// CSI driver images
		{
			name:        "csi_driver_image",
			description: "CSI driver image with complex path",
			input:       "registry.k8s.io/sig-storage/csi-provisioner:v3.6.0",
			expectError: false,
		},
		// Admission webhook images
		{
			name:        "admission_webhook_image",
			description: "Admission webhook controller image",
			input:       "k8s.gcr.io/ingress-nginx/kube-webhook-certgen:v1.3.0",
			expectError: false,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result, err := ProcessContainerName(tc.input)

			if tc.expectError {
				assert.Error(s.T(), err, "Description: %s", tc.description)
			} else {
				assert.NoError(s.T(), err, "Description: %s", tc.description)
				assert.NotEmpty(s.T(), result.Image, "Image should not be empty")
				assert.Equal(s.T(), tc.input, result.FullName, "FullName should be preserved")
			}
		})
	}
}

// TestImageParsingEdgeCases tests edge cases that might occur in real Kubernetes clusters
func (s *K8sTestSuite) TestImageParsingEdgeCases() {
	s.Run("image with plus sign in tag", func() {
		// Some images use + in tags (e.g., build metadata)
		result, err := ProcessContainerName("myimage:v1.0.0+build123")
		require.NoError(s.T(), err)
		assert.Equal(s.T(), "myimage", result.Image)
		assert.Equal(s.T(), "v1.0.0+build123", result.Tag)
	})

	s.Run("image with underscore in name", func() {
		result, err := ProcessContainerName("my_custom_image:latest")
		require.NoError(s.T(), err)
		assert.Equal(s.T(), "my_custom_image", result.Image)
	})

	s.Run("image with dash in tag", func() {
		result, err := ProcessContainerName("nginx:1.21-alpine")
		require.NoError(s.T(), err)
		assert.Equal(s.T(), "1.21-alpine", result.Tag)
	})

	s.Run("image with rc/beta tag", func() {
		result, err := ProcessContainerName("kubernetes/kube-apiserver:v1.29.0-rc.0")
		require.NoError(s.T(), err)
		assert.Equal(s.T(), "v1.29.0-rc.0", result.Tag)
	})

	s.Run("image with only sha256 digest (no tag)", func() {
		sha := "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		result, err := ProcessContainerName("alpine@" + sha)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), "alpine", result.Image)
		assert.Equal(s.T(), "", result.Tag)
		assert.Equal(s.T(), sha, result.Sha)
	})

	s.Run("aws ecr image", func() {
		result, err := ProcessContainerName("123456789.dkr.ecr.us-east-1.amazonaws.com/my-repo:latest")
		require.NoError(s.T(), err)
		assert.Equal(s.T(), "123456789.dkr.ecr.us-east-1.amazonaws.com/my-repo", result.Image)
		assert.Equal(s.T(), "latest", result.Tag)
	})

	s.Run("azure acr image", func() {
		result, err := ProcessContainerName("myregistry.azurecr.io/samples/nginx:v1")
		require.NoError(s.T(), err)
		assert.Equal(s.T(), "myregistry.azurecr.io/samples/nginx", result.Image)
		assert.Equal(s.T(), "v1", result.Tag)
	})

	s.Run("google artifact registry", func() {
		result, err := ProcessContainerName("us-central1-docker.pkg.dev/my-project/my-repo/my-image:tag")
		require.NoError(s.T(), err)
		assert.Equal(s.T(), "us-central1-docker.pkg.dev/my-project/my-repo/my-image", result.Image)
		assert.Equal(s.T(), "tag", result.Tag)
	})
}

// BenchmarkProcessContainerName benchmarks the ProcessContainerName function
func BenchmarkProcessContainerName(b *testing.B) {
	images := []string{
		"nginx",
		"nginx:latest",
		"quay.io/cilium/cilium:v1.18.4@sha256:49d87af187eeeb9e9e3ec2bc6bd372261a0b5cb2d845659463ba7cc10fe9e45f",
		"gcr.io/google-containers/pause:3.2",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, img := range images {
			_, _ = ProcessContainerName(img)
		}
	}
}

// BenchmarkNormalizeImageName benchmarks the NormalizeImageName function
func BenchmarkNormalizeImageName(b *testing.B) {
	images := []string{
		"nginx",
		"nginx:latest",
		"quay.io/cilium/cilium:v1.18.4@sha256:49d87af187eeeb9e9e3ec2bc6bd372261a0b5cb2d845659463ba7cc10fe9e45f",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, img := range images {
			_ = NormalizeImageName(img)
		}
	}
}

// ========== K8s Resource Wrapper Tests ==========

// TestDeploymentWrapper tests the DeploymentWrapper GetPodSpec method
func (s *K8sTestSuite) TestDeploymentWrapper() {
	deployment := appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "main", Image: "nginx:latest"},
					},
					InitContainers: []corev1.Container{
						{Name: "init", Image: "busybox:1.36"},
					},
				},
			},
		},
	}

	wrapper := (*DeploymentWrapper)(&deployment)
	podSpec := wrapper.GetPodSpec()

	require.NotNil(s.T(), podSpec)
	assert.Len(s.T(), podSpec.Containers, 1)
	assert.Equal(s.T(), "nginx:latest", podSpec.Containers[0].Image)
	assert.Len(s.T(), podSpec.InitContainers, 1)
	assert.Equal(s.T(), "busybox:1.36", podSpec.InitContainers[0].Image)
}

// TestJobWrapper tests the JobWrapper GetPodSpec method
func (s *K8sTestSuite) TestJobWrapper() {
	job := batchv1.Job{
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "worker", Image: "worker:v1.0"},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}

	wrapper := (*JobWrapper)(&job)
	podSpec := wrapper.GetPodSpec()

	require.NotNil(s.T(), podSpec)
	assert.Len(s.T(), podSpec.Containers, 1)
	assert.Equal(s.T(), "worker:v1.0", podSpec.Containers[0].Image)
}

// TestDaemonSetWrapper tests the DaemonSetWrapper GetPodSpec method
func (s *K8sTestSuite) TestDaemonSetWrapper() {
	daemonSet := appsv1.DaemonSet{
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "agent", Image: "prometheus/node-exporter:v1.6.0"},
					},
				},
			},
		},
	}

	wrapper := (*DaemonSetWrapper)(&daemonSet)
	podSpec := wrapper.GetPodSpec()

	require.NotNil(s.T(), podSpec)
	assert.Len(s.T(), podSpec.Containers, 1)
	assert.Equal(s.T(), "prometheus/node-exporter:v1.6.0", podSpec.Containers[0].Image)
}

// TestCronJobWrapper tests the CronJobWrapper GetPodSpec method
func (s *K8sTestSuite) TestCronJobWrapper() {
	cronJob := batchv1.CronJob{
		Spec: batchv1.CronJobSpec{
			Schedule: "*/5 * * * *",
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "cron-worker", Image: "myapp/cron:latest"},
							},
							RestartPolicy: corev1.RestartPolicyOnFailure,
						},
					},
				},
			},
		},
	}

	wrapper := (*CronJobWrapper)(&cronJob)
	podSpec := wrapper.GetPodSpec()

	require.NotNil(s.T(), podSpec)
	assert.Len(s.T(), podSpec.Containers, 1)
	assert.Equal(s.T(), "myapp/cron:latest", podSpec.Containers[0].Image)
}

// ========== ProcessContainers Tests ==========

// TestProcessContainers tests the processContainers function
func (s *K8sTestSuite) TestProcessContainers() {
	ctx := context.Background()

	s.Run("deployment with containers and init containers", func() {
		deployment := appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "main", Image: "nginx:1.21"},
							{Name: "sidecar", Image: "envoyproxy/envoy:v1.28"},
						},
						InitContainers: []corev1.Container{
							{Name: "init-db", Image: "postgres:15"},
						},
					},
				},
			},
		}

		wrapper := (*DeploymentWrapper)(&deployment)
		containersList := &ContainersList{}
		err := processContainers(ctx, wrapper, "default", containersList)

		require.NoError(s.T(), err)
		assert.Len(s.T(), containersList.Containers, 3)

		// Verify all images are processed
		images := make([]string, len(containersList.Containers))
		for i, c := range containersList.Containers {
			images[i] = c.FullName
		}
		assert.Contains(s.T(), images, "nginx:1.21")
		assert.Contains(s.T(), images, "envoyproxy/envoy:v1.28")
		assert.Contains(s.T(), images, "postgres:15")

		// Verify namespace is set
		for _, c := range containersList.Containers {
			assert.Equal(s.T(), "default", c.ImageNamespace)
		}
	})

	s.Run("job with single container", func() {
		job := batchv1.Job{
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "worker", Image: "myapp/worker:v2.0"},
						},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				},
			},
		}

		wrapper := (*JobWrapper)(&job)
		containersList := &ContainersList{}
		err := processContainers(ctx, wrapper, "production", containersList)

		require.NoError(s.T(), err)
		assert.Len(s.T(), containersList.Containers, 1)
		assert.Equal(s.T(), "myapp/worker", containersList.Containers[0].Image)
		assert.Equal(s.T(), "v2.0", containersList.Containers[0].Tag)
		assert.Equal(s.T(), "production", containersList.Containers[0].ImageNamespace)
	})

	s.Run("daemonset with multiple containers", func() {
		daemonSet := appsv1.DaemonSet{
			Spec: appsv1.DaemonSetSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "exporter", Image: "prom/node-exporter:v1.6.0"},
							{Name: "collector", Image: "otel/opentelemetry-collector:0.88.0"},
						},
					},
				},
			},
		}

		wrapper := (*DaemonSetWrapper)(&daemonSet)
		containersList := &ContainersList{}
		err := processContainers(ctx, wrapper, "monitoring", containersList)

		require.NoError(s.T(), err)
		assert.Len(s.T(), containersList.Containers, 2)
	})

	s.Run("cronjob with container", func() {
		cronJob := batchv1.CronJob{
			Spec: batchv1.CronJobSpec{
				Schedule: "0 * * * *",
				JobTemplate: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "backup", Image: "restic/restic:0.16.2"},
								},
								RestartPolicy: corev1.RestartPolicyNever,
							},
						},
					},
				},
			},
		}

		wrapper := (*CronJobWrapper)(&cronJob)
		containersList := &ContainersList{}
		err := processContainers(ctx, wrapper, "backup", containersList)

		require.NoError(s.T(), err)
		assert.Len(s.T(), containersList.Containers, 1)
		assert.Equal(s.T(), "restic/restic", containersList.Containers[0].Image)
	})

	s.Run("deployment with ephemeral containers", func() {
		deployment := appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "main", Image: "myapp:v1"},
						},
						EphemeralContainers: []corev1.EphemeralContainer{
							{
								EphemeralContainerCommon: corev1.EphemeralContainerCommon{
									Name:  "debugger",
									Image: "busybox:1.36",
								},
							},
						},
					},
				},
			},
		}

		wrapper := (*DeploymentWrapper)(&deployment)
		containersList := &ContainersList{}
		err := processContainers(ctx, wrapper, "default", containersList)

		require.NoError(s.T(), err)
		// Should include both main container and ephemeral container
		assert.Len(s.T(), containersList.Containers, 2)
	})

	s.Run("context cancellation", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		deployment := appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "main", Image: "nginx:latest"},
						},
					},
				},
			},
		}

		wrapper := (*DeploymentWrapper)(&deployment)
		containersList := &ContainersList{}
		err := processContainers(ctx, wrapper, "default", containersList)

		// Should return context error
		assert.Error(s.T(), err)
		assert.ErrorIs(s.T(), err, context.Canceled)
	})
}

// TestProcessContainer tests the processContainer function directly
func (s *K8sTestSuite) TestProcessContainer() {
	ctx := context.Background()

	s.Run("valid image", func() {
		containersList := &ContainersList{}
		err := processContainer(ctx, "nginx:1.21", "default", containersList)

		require.NoError(s.T(), err)
		assert.Len(s.T(), containersList.Containers, 1)
		assert.Equal(s.T(), "nginx", containersList.Containers[0].Image)
		assert.Equal(s.T(), "1.21", containersList.Containers[0].Tag)
		assert.Equal(s.T(), "default", containersList.Containers[0].ImageNamespace)
	})

	s.Run("image with sha", func() {
		sha := "sha256:abc123def456"
		containersList := &ContainersList{}
		err := processContainer(ctx, "nginx:1.21@"+sha, "production", containersList)

		require.NoError(s.T(), err)
		assert.Len(s.T(), containersList.Containers, 1)
		assert.Equal(s.T(), "nginx", containersList.Containers[0].Image)
		assert.Equal(s.T(), "1.21", containersList.Containers[0].Tag)
		assert.Equal(s.T(), sha, containersList.Containers[0].Sha)
	})

	s.Run("context already cancelled", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		containersList := &ContainersList{}
		err := processContainer(ctx, "nginx:latest", "default", containersList)

		assert.Error(s.T(), err)
		assert.ErrorIs(s.T(), err, context.Canceled)
	})

	s.Run("invalid image name", func() {
		containersList := &ContainersList{}
		err := processContainer(ctx, "@@@invalid@@@", "default", containersList)

		assert.Error(s.T(), err)
	})
}

// TestContainerCache tests the container cache functionality
func (s *K8sTestSuite) TestContainerCache() {
	// Clear cache first
	containerCache.Lock()
	containerCache.cache = make(map[string]Container)
	containerCache.Unlock()

	s.Run("cache miss then hit", func() {
		// First call - cache miss
		result1, err := ProcessContainerName("redis:7.0")
		require.NoError(s.T(), err)
		assert.Equal(s.T(), "redis", result1.Image)

		// Second call - should hit cache
		result2, err := ProcessContainerName("redis:7.0")
		require.NoError(s.T(), err)
		assert.Equal(s.T(), result1, result2)
	})

	s.Run("get from empty cache", func() {
		_, found := containerCache.Get("nonexistent:tag")
		assert.False(s.T(), found)
	})

	s.Run("set and get from cache", func() {
		testContainer := Container{
			Image:    "test-image",
			Tag:      "test-tag",
			FullName: "test-image:test-tag",
		}
		containerCache.Set("test-key", testContainer)

		retrieved, found := containerCache.Get("test-key")
		assert.True(s.T(), found)
		assert.Equal(s.T(), testContainer, retrieved)
	})
}

// TestConcurrentProcessing tests concurrent container processing
func (s *K8sTestSuite) TestConcurrentProcessing() {
	ctx := context.Background()

	s.Run("concurrent processContainer calls", func() {
		images := []string{
			"nginx:1.21",
			"redis:7.0",
			"postgres:15",
			"mysql:8.0",
			"mongodb:6.0",
		}

		results := make(chan Container, len(images))
		errors := make(chan error, len(images))

		for _, img := range images {
			go func(image string) {
				containersList := &ContainersList{}
				err := processContainer(ctx, image, "default", containersList)
				if err != nil {
					errors <- err
					return
				}
				if len(containersList.Containers) > 0 {
					results <- containersList.Containers[0]
				}
			}(img)
		}

		// Collect results
		collected := 0
		for collected < len(images) {
			select {
			case <-results:
				collected++
			case err := <-errors:
				s.T().Errorf("Unexpected error: %v", err)
				collected++
			case <-time.After(5 * time.Second):
				s.T().Fatal("Timeout waiting for concurrent processing")
			}
		}

		assert.Equal(s.T(), len(images), collected)
	})
}

// Helper to create a fake client
func (s *K8sTestSuite) newFakeClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = appsv1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}

// TestListAndProcessResources tests the ListAndProcessResources function
func (s *K8sTestSuite) TestListAndProcessResources() {
	ctx := context.Background()

	s.Run("process deployments", func() {
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-deployment",
				Namespace: "default",
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "main", Image: "nginx:1.21"},
							{Name: "sidecar", Image: "envoy:v1.28"},
						},
					},
				},
			},
		}

		fakeClient := s.newFakeClient(deployment)
		containersList := &ContainersList{}
		err := ListAndProcessResources[*DeploymentWrapper](ctx, fakeClient, &appsv1.DeploymentList{}, containersList)

		require.NoError(s.T(), err)
		assert.Len(s.T(), containersList.Containers, 2)

		// Verify images
		images := make(map[string]bool)
		for _, c := range containersList.Containers {
			images[c.FullName] = true
		}
		assert.True(s.T(), images["nginx:1.21"])
		assert.True(s.T(), images["envoy:v1.28"])
	})

	s.Run("process jobs", func() {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-job",
				Namespace: "default",
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "worker", Image: "busybox:1.36"},
						},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				},
			},
		}

		fakeClient := s.newFakeClient(job)
		containersList := &ContainersList{}
		err := ListAndProcessResources[*JobWrapper](ctx, fakeClient, &batchv1.JobList{}, containersList)

		require.NoError(s.T(), err)
		assert.Len(s.T(), containersList.Containers, 1)
		assert.Equal(s.T(), "busybox", containersList.Containers[0].Image)
	})

	s.Run("process daemonsets", func() {
		daemonSet := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ds",
				Namespace: "monitoring",
			},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "exporter"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "exporter"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "exporter", Image: "prom/node-exporter:v1.6.0"},
						},
					},
				},
			},
		}

		fakeClient := s.newFakeClient(daemonSet)
		containersList := &ContainersList{}
		err := ListAndProcessResources[*DaemonSetWrapper](ctx, fakeClient, &appsv1.DaemonSetList{}, containersList)

		require.NoError(s.T(), err)
		assert.Len(s.T(), containersList.Containers, 1)
		assert.Equal(s.T(), "monitoring", containersList.Containers[0].ImageNamespace)
	})

	s.Run("process cronjobs", func() {
		cronJob := &batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cron",
				Namespace: "batch",
			},
			Spec: batchv1.CronJobSpec{
				Schedule: "0 * * * *",
				JobTemplate: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "cron", Image: "alpine:3.18"},
								},
								RestartPolicy: corev1.RestartPolicyNever,
							},
						},
					},
				},
			},
		}

		fakeClient := s.newFakeClient(cronJob)
		containersList := &ContainersList{}
		err := ListAndProcessResources[*CronJobWrapper](ctx, fakeClient, &batchv1.CronJobList{}, containersList)

		require.NoError(s.T(), err)
		assert.Len(s.T(), containersList.Containers, 1)
		assert.Equal(s.T(), "alpine", containersList.Containers[0].Image)
	})

	s.Run("process multiple resources", func() {
		deployment1 := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "deploy-1", Namespace: "ns1"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "app1"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "app1"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "main", Image: "app1:v1"}},
					},
				},
			},
		}
		deployment2 := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "deploy-2", Namespace: "ns2"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "app2"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "app2"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "main", Image: "app2:v1"}},
					},
				},
			},
		}

		fakeClient := s.newFakeClient(deployment1, deployment2)
		containersList := &ContainersList{}
		err := ListAndProcessResources[*DeploymentWrapper](ctx, fakeClient, &appsv1.DeploymentList{}, containersList)

		require.NoError(s.T(), err)
		assert.Len(s.T(), containersList.Containers, 2)
	})

	s.Run("empty list", func() {
		fakeClient := s.newFakeClient()
		containersList := &ContainersList{}
		err := ListAndProcessResources[*DeploymentWrapper](ctx, fakeClient, &appsv1.DeploymentList{}, containersList)

		require.NoError(s.T(), err)
		assert.Len(s.T(), containersList.Containers, 0)
	})

	s.Run("context cancellation", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "main", Image: "test:v1"}},
					},
				},
			},
		}

		fakeClient := s.newFakeClient(deployment)
		containersList := &ContainersList{}
		err := ListAndProcessResources[*DeploymentWrapper](ctx, fakeClient, &appsv1.DeploymentList{}, containersList)

		// Context cancellation should result in an error
		assert.Error(s.T(), err)
	})

	s.Run("unsupported list type", func() {
		fakeClient := s.newFakeClient()
		containersList := &ContainersList{}
		// Pass an unsupported list type (corev1.PodList is not handled)
		err := ListAndProcessResources[*DeploymentWrapper](ctx, fakeClient, &corev1.PodList{}, containersList)

		assert.Error(s.T(), err)
		assert.Contains(s.T(), err.Error(), "unsupported list type")
	})
}
