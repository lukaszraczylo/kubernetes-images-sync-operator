package shared

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// TestScenario represents a test scenario classification
type TestScenario string

const (
	ScenarioGood      TestScenario = "good"
	ScenarioNotGood   TestScenario = "not_good"
	ScenarioReallyBad TestScenario = "really_bad"
)

// DefinitionsTestSuite tests the definitions and utility functions
type DefinitionsTestSuite struct {
	suite.Suite
}

func TestDefinitionsTestSuite(t *testing.T) {
	suite.Run(t, new(DefinitionsTestSuite))
}

// TestNormalizeImageName tests the NormalizeImageName function with matrix strategy
func (s *DefinitionsTestSuite) TestNormalizeImageName() {
	testCases := []struct {
		name     string
		scenario TestScenario
		input    string
		expected string
	}{
		// Good scenarios - standard image names
		{
			name:     "simple image name",
			scenario: ScenarioGood,
			input:    "nginx",
			expected: "nginx",
		},
		{
			name:     "image with tag",
			scenario: ScenarioGood,
			input:    "nginx:latest",
			expected: "nginx-latest",
		},
		{
			name:     "image with version tag",
			scenario: ScenarioGood,
			input:    "nginx:1.21.0",
			expected: "nginx-1.21.0",
		},
		{
			name:     "full registry path",
			scenario: ScenarioGood,
			input:    "quay.io/cilium/cilium:v1.18.4",
			expected: "quay.io-cilium-cilium-v1.18.4",
		},
		{
			name:     "ghcr registry",
			scenario: ScenarioGood,
			input:    "ghcr.io/owner/repo:v1.0.0",
			expected: "ghcr.io-owner-repo-v1.0.0",
		},

		// Not good scenarios - unusual but valid formats
		{
			name:     "image with SHA digest",
			scenario: ScenarioNotGood,
			input:    "nginx@sha256:abc123def456",
			expected: "nginx-sha256-abc123def456",
		},
		{
			name:     "image with tag and SHA",
			scenario: ScenarioNotGood,
			input:    "quay.io/cilium/cilium:v1.18.4@sha256:49d87af187eeeb9e9e3ec2bc6bd372261a0b5cb2d845659463ba7cc10fe9e45f",
			expected: "quay.io-cilium-cilium-v1.18.4-sha256-49d87af187eeeb9e9e3ec2bc6bd372261a0b5cb2d845659463ba7cc10fe9e45f",
		},
		{
			name:     "multiple colons in path",
			scenario: ScenarioNotGood,
			input:    "registry:5000/image:tag",
			expected: "registry-5000-image-tag",
		},

		// Really bad scenarios - edge cases and potential problems
		{
			name:     "empty string",
			scenario: ScenarioReallyBad,
			input:    "",
			expected: "",
		},
		{
			name:     "only special characters",
			scenario: ScenarioReallyBad,
			input:    ":///@",
			expected: "",
		},
		{
			name:     "multiple consecutive special chars",
			scenario: ScenarioReallyBad,
			input:    "image:::tag",
			expected: "image-tag",
		},
		{
			name:     "leading special characters",
			scenario: ScenarioReallyBad,
			input:    "//image:tag",
			expected: "image-tag",
		},
		{
			name:     "trailing special characters",
			scenario: ScenarioReallyBad,
			input:    "image:tag//",
			expected: "image-tag",
		},
		{
			name:     "spaces in name",
			scenario: ScenarioReallyBad,
			input:    "image name:tag",
			expected: "image-name-tag",
		},
		{
			name:     "unicode characters",
			scenario: ScenarioReallyBad,
			input:    "image:tag-日本語",
			expected: "image-tag-日本語",
		},
		{
			name:     "very long image name",
			scenario: ScenarioReallyBad,
			input:    "registry.example.com/very/long/path/to/image:tag@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expected: "registry.example.com-very-long-path-to-image-tag-sha256-abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		},
		{
			name:     "query string characters",
			scenario: ScenarioReallyBad,
			input:    "image?foo=bar&baz=qux",
			expected: "image-foo-bar-baz-qux",
		},
		{
			name:     "brackets and special chars",
			scenario: ScenarioReallyBad,
			input:    "image[tag]{version}",
			expected: "image-tag-version",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result := NormalizeImageName(tc.input)
			assert.Equal(s.T(), tc.expected, result, "Scenario: %s", tc.scenario)
		})
	}
}

// TestRemoveDuplicates tests duplicate removal functionality
func (s *DefinitionsTestSuite) TestRemoveDuplicates() {
	testCases := []struct {
		name     string
		scenario TestScenario
		input    ContainersList
		expected int
	}{
		// Good scenarios
		{
			name:     "no duplicates",
			scenario: ScenarioGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", Tag: "latest", FullName: "nginx:latest"},
					{Image: "redis", Tag: "6", FullName: "redis:6"},
				},
			},
			expected: 2,
		},
		{
			name:     "with duplicates",
			scenario: ScenarioGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", Tag: "latest", FullName: "nginx:latest"},
					{Image: "nginx", Tag: "latest", FullName: "nginx:latest"},
					{Image: "redis", Tag: "6", FullName: "redis:6"},
				},
			},
			expected: 2,
		},

		// Not good scenarios
		{
			name:     "same image different tags",
			scenario: ScenarioNotGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", Tag: "latest", FullName: "nginx:latest"},
					{Image: "nginx", Tag: "1.21", FullName: "nginx:1.21"},
				},
			},
			expected: 2, // Should keep both
		},
		{
			name:     "same image with and without SHA",
			scenario: ScenarioNotGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", Tag: "latest", FullName: "nginx:latest"},
					{Image: "nginx", Tag: "latest", Sha: "sha256:abc", FullName: "nginx:latest@sha256:abc"},
				},
			},
			expected: 2, // Different because of SHA
		},

		// Really bad scenarios
		{
			name:     "empty list",
			scenario: ScenarioReallyBad,
			input:    ContainersList{Containers: []Container{}},
			expected: 0,
		},
		{
			name:     "all duplicates",
			scenario: ScenarioReallyBad,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", Tag: "latest", FullName: "nginx:latest"},
					{Image: "nginx", Tag: "latest", FullName: "nginx:latest"},
					{Image: "nginx", Tag: "latest", FullName: "nginx:latest"},
				},
			},
			expected: 1,
		},
		{
			name:     "nil containers",
			scenario: ScenarioReallyBad,
			input:    ContainersList{Containers: nil},
			expected: 0,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result := RemoveDuplicates(tc.input)
			assert.Len(s.T(), result.Containers, tc.expected, "Scenario: %s", tc.scenario)
		})
	}
}

// TestRemoveExcludedImages tests image exclusion functionality
func (s *DefinitionsTestSuite) TestRemoveExcludedImages() {
	testCases := []struct {
		name     string
		scenario TestScenario
		input    ContainersList
		excludes []string
		expected int
	}{
		// Good scenarios
		{
			name:     "no exclusions",
			scenario: ScenarioGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", Tag: "latest"},
					{Image: "redis", Tag: "6"},
				},
			},
			excludes: []string{},
			expected: 2,
		},
		{
			name:     "exclude one image",
			scenario: ScenarioGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", Tag: "latest"},
					{Image: "redis", Tag: "6"},
				},
			},
			excludes: []string{"nginx"},
			expected: 1,
		},
		{
			name:     "exclude by registry",
			scenario: ScenarioGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "gcr.io/google/nginx", Tag: "latest"},
					{Image: "docker.io/library/redis", Tag: "6"},
				},
			},
			excludes: []string{"gcr.io"},
			expected: 1,
		},

		// Not good scenarios
		{
			name:     "case insensitive exclusion",
			scenario: ScenarioNotGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "NGINX", Tag: "latest"},
					{Image: "Redis", Tag: "6"},
				},
			},
			excludes: []string{"nginx"},
			expected: 1, // Should exclude NGINX
		},
		{
			name:     "partial match exclusion",
			scenario: ScenarioNotGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "my-nginx-custom", Tag: "latest"},
					{Image: "redis", Tag: "6"},
				},
			},
			excludes: []string{"nginx"},
			expected: 1, // Should exclude my-nginx-custom
		},

		// Really bad scenarios
		{
			name:     "exclude all images",
			scenario: ScenarioReallyBad,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", Tag: "latest"},
					{Image: "redis", Tag: "6"},
				},
			},
			excludes: []string{"nginx", "redis"},
			expected: 0,
		},
		{
			name:     "empty exclude list on empty containers",
			scenario: ScenarioReallyBad,
			input:    ContainersList{Containers: []Container{}},
			excludes: []string{},
			expected: 0,
		},
		{
			name:     "exclude with empty string",
			scenario: ScenarioReallyBad,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", Tag: "latest"},
				},
			},
			excludes: []string{""},
			expected: 0, // Empty string matches all
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result := RemoveExcludedImages(tc.input, tc.excludes)
			assert.Len(s.T(), result.Containers, tc.expected, "Scenario: %s", tc.scenario)
		})
	}
}

// TestIncludeOnlyImages tests image inclusion filtering
func (s *DefinitionsTestSuite) TestIncludeOnlyImages() {
	testCases := []struct {
		name     string
		scenario TestScenario
		input    ContainersList
		includes []string
		expected int
	}{
		// Good scenarios
		{
			name:     "include specific image",
			scenario: ScenarioGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", Tag: "latest"},
					{Image: "redis", Tag: "6"},
					{Image: "postgres", Tag: "14"},
				},
			},
			includes: []string{"nginx"},
			expected: 1,
		},
		{
			name:     "include multiple images",
			scenario: ScenarioGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", Tag: "latest"},
					{Image: "redis", Tag: "6"},
					{Image: "postgres", Tag: "14"},
				},
			},
			includes: []string{"nginx", "redis"},
			expected: 2,
		},

		// Not good scenarios
		{
			name:     "include by partial match",
			scenario: ScenarioNotGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "my-nginx-app", Tag: "latest"},
					{Image: "nginx-proxy", Tag: "v1"},
					{Image: "redis", Tag: "6"},
				},
			},
			includes: []string{"nginx"},
			expected: 2,
		},

		// Really bad scenarios
		{
			name:     "include non-existent image",
			scenario: ScenarioReallyBad,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", Tag: "latest"},
				},
			},
			includes: []string{"nonexistent"},
			expected: 0,
		},
		{
			name:     "empty includes list",
			scenario: ScenarioReallyBad,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", Tag: "latest"},
				},
			},
			includes: []string{},
			expected: 0,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result := IncludeOnlyImages(tc.input, tc.includes)
			assert.Len(s.T(), result.Containers, tc.expected, "Scenario: %s", tc.scenario)
		})
	}
}

// TestFilterOnlyFromNamespaces tests namespace filtering
func (s *DefinitionsTestSuite) TestFilterOnlyFromNamespaces() {
	testCases := []struct {
		name       string
		scenario   TestScenario
		input      ContainersList
		namespaces []string
		expected   int
	}{
		// Good scenarios
		{
			name:     "filter single namespace",
			scenario: ScenarioGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", ImageNamespace: "default"},
					{Image: "redis", ImageNamespace: "kube-system"},
				},
			},
			namespaces: []string{"default"},
			expected:   1,
		},

		// Not good scenarios
		{
			name:     "filter multiple namespaces",
			scenario: ScenarioNotGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", ImageNamespace: "default"},
					{Image: "redis", ImageNamespace: "kube-system"},
					{Image: "postgres", ImageNamespace: "database"},
				},
			},
			namespaces: []string{"default", "database"},
			expected:   2,
		},

		// Really bad scenarios
		{
			name:     "filter non-existent namespace",
			scenario: ScenarioReallyBad,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", ImageNamespace: "default"},
				},
			},
			namespaces: []string{"nonexistent"},
			expected:   0,
		},
		{
			name:     "empty namespace filter",
			scenario: ScenarioReallyBad,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", ImageNamespace: "default"},
				},
			},
			namespaces: []string{},
			expected:   0,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result := FilterOnlyFromNamespaces(tc.input, tc.namespaces)
			assert.Len(s.T(), result.Containers, tc.expected, "Scenario: %s", tc.scenario)
		})
	}
}

// TestFilterOutWholeNamespaces tests namespace exclusion
func (s *DefinitionsTestSuite) TestFilterOutWholeNamespaces() {
	testCases := []struct {
		name       string
		scenario   TestScenario
		input      ContainersList
		namespaces []string
		expected   int
	}{
		// Good scenarios
		{
			name:     "exclude kube-system",
			scenario: ScenarioGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", ImageNamespace: "default"},
					{Image: "coredns", ImageNamespace: "kube-system"},
					{Image: "redis", ImageNamespace: "apps"},
				},
			},
			namespaces: []string{"kube-system"},
			expected:   2,
		},

		// Not good scenarios
		{
			name:     "exclude multiple system namespaces",
			scenario: ScenarioNotGood,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", ImageNamespace: "default"},
					{Image: "coredns", ImageNamespace: "kube-system"},
					{Image: "cilium", ImageNamespace: "kube-system"},
					{Image: "local-path", ImageNamespace: "local-path-storage"},
				},
			},
			namespaces: []string{"kube-system", "local-path-storage"},
			expected:   1,
		},

		// Really bad scenarios
		{
			name:     "exclude all namespaces",
			scenario: ScenarioReallyBad,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", ImageNamespace: "default"},
					{Image: "redis", ImageNamespace: "apps"},
				},
			},
			namespaces: []string{"default", "apps"},
			expected:   0,
		},
		{
			name:     "empty exclusion list",
			scenario: ScenarioReallyBad,
			input: ContainersList{
				Containers: []Container{
					{Image: "nginx", ImageNamespace: "default"},
				},
			},
			namespaces: []string{},
			expected:   1, // No exclusions = keep all
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result := FilterOutWholeNamespaces(tc.input, tc.namespaces)
			assert.Len(s.T(), result.Containers, tc.expected, "Scenario: %s", tc.scenario)
		})
	}
}

// TestContainerStruct tests Container struct behavior
func (s *DefinitionsTestSuite) TestContainerStruct() {
	s.Run("full container with all fields", func() {
		c := Container{
			Image:          "quay.io/cilium/cilium",
			Tag:            "v1.18.4",
			Sha:            "sha256:49d87af187eeeb9e9e3ec2bc6bd372261a0b5cb2d845659463ba7cc10fe9e45f",
			FullName:       "quay.io/cilium/cilium:v1.18.4@sha256:49d87af187eeeb9e9e3ec2bc6bd372261a0b5cb2d845659463ba7cc10fe9e45f",
			ImageNamespace: "kube-system",
		}

		assert.Equal(s.T(), "quay.io/cilium/cilium", c.Image)
		assert.Equal(s.T(), "v1.18.4", c.Tag)
		assert.Contains(s.T(), c.Sha, "sha256:")
		assert.Contains(s.T(), c.FullName, "@")
	})

	s.Run("container equality for deduplication", func() {
		c1 := Container{Image: "nginx", Tag: "latest", FullName: "nginx:latest"}
		c2 := Container{Image: "nginx", Tag: "latest", FullName: "nginx:latest"}
		c3 := Container{Image: "nginx", Tag: "1.21", FullName: "nginx:1.21"}

		assert.Equal(s.T(), c1, c2)
		assert.NotEqual(s.T(), c1, c3)
	})
}

// TestConstants tests that constants are defined correctly
func (s *DefinitionsTestSuite) TestConstants() {
	// Status constants
	assert.Equal(s.T(), "PENDING", STATUS_PENDING)
	assert.Equal(s.T(), "STARTING", STATUS_STARTING)
	assert.Equal(s.T(), "RETRYING", STATUS_RETRYING)
	assert.Equal(s.T(), "RUNNING", STATUS_RUNNING)
	assert.Equal(s.T(), "FAILED", STATUS_FAILED)
	assert.Equal(s.T(), "COMPLETED", STATUS_SUCCESS)
	assert.Equal(s.T(), "PRESENT", STATUS_PRESENT)

	// Storage constants
	assert.Equal(s.T(), "S3", STORAGE_S3)
	assert.Equal(s.T(), "FILE", STORAGE_FILE)
}

// TestBackupJobImage tests the BACKUP_JOB_IMAGE initialization
func (s *DefinitionsTestSuite) TestBackupJobImage() {
	require.NotEmpty(s.T(), BACKUP_JOB_IMAGE)
	assert.Contains(s.T(), BACKUP_JOB_IMAGE, "kubernetes-images-sync-worker")
}
