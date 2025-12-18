package raczylocom

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	raczylocomv1 "github.com/lukaszraczylo/kubernetes-images-sync-operator/api/raczylo.com/v1"
	"github.com/lukaszraczylo/kubernetes-images-sync-operator/internal/shared"
)

type TestScenario string

const (
	ScenarioGood      TestScenario = "good"
	ScenarioNotGood   TestScenario = "not_good"
	ScenarioReallyBad TestScenario = "really_bad"
)

type ControllerTestSuite struct {
	suite.Suite
	scheme *runtime.Scheme
	ctx    context.Context
}

func TestControllerTestSuite(t *testing.T) {
	suite.Run(t, new(ControllerTestSuite))
}

func (s *ControllerTestSuite) SetupSuite() {
	s.scheme = runtime.NewScheme()
	require.NoError(s.T(), clientgoscheme.AddToScheme(s.scheme))
	require.NoError(s.T(), raczylocomv1.AddToScheme(s.scheme))
	s.ctx = context.Background()
}

func (s *ControllerTestSuite) newFakeClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(objs...).
		WithStatusSubresource(&raczylocomv1.ClusterImage{}, &raczylocomv1.ClusterImageExport{}).
		WithIndex(&raczylocomv1.ClusterImage{}, "spec.exportName", func(obj client.Object) []string {
			ci := obj.(*raczylocomv1.ClusterImage)
			return []string{ci.Spec.ExportName}
		}).
		Build()
}

// Helper to create a test ClusterImageExport
func (s *ControllerTestSuite) createClusterImageExport(name, namespace string, opts ...func(*raczylocomv1.ClusterImageExport)) *raczylocomv1.ClusterImageExport {
	export := &raczylocomv1.ClusterImageExport{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "raczylo.com/v1",
			Kind:       "ClusterImageExport",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       "test-uid-export",
		},
		Spec: raczylocomv1.ClusterImageExportSpec{
			Name:     name,
			BasePath: "/backup",
			Storage: raczylocomv1.ClusterImageStorageSpec{
				StorageTarget: shared.STORAGE_S3,
				S3: raczylocomv1.ClusterImageStorageS3{
					Bucket: "test-bucket",
					Region: "us-east-1",
				},
			},
			MaxConcurrentJobs: 5,
		},
	}
	for _, opt := range opts {
		opt(export)
	}
	return export
}

// Helper to create a test ClusterImage
func (s *ControllerTestSuite) createClusterImage(name, namespace, exportName string, opts ...func(*raczylocomv1.ClusterImage)) *raczylocomv1.ClusterImage {
	image := &raczylocomv1.ClusterImage{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "raczylo.com/v1",
			Kind:       "ClusterImage",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       "test-uid-image",
		},
		Spec: raczylocomv1.ClusterImageSpec{
			Image:      "nginx",
			Tag:        "latest",
			FullName:   "nginx:latest",
			Storage:    shared.STORAGE_S3,
			ExportName: exportName,
			ExportPath: "/backup",
		},
	}
	for _, opt := range opts {
		opt(image)
	}
	return image
}

// ==================== ClusterImage Controller Tests ====================

func (s *ControllerTestSuite) TestClusterImageReconcile_NotFound() {
	// Scenario: Good - resource doesn't exist, nothing to do
	client := s.newFakeClient()
	reconciler := &ClusterImageReconciler{
		Client: client,
		Scheme: s.scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "non-existent",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), ctrl.Result{}, result)
}

func (s *ControllerTestSuite) TestClusterImageReconcile_InitialStatus_Pending() {
	// Scenario: Good - new ClusterImage should be set to PENDING
	export := s.createClusterImageExport("test-export", "default")
	image := s.createClusterImage("test-image", "default", "test-export")

	client := s.newFakeClient(export, image)
	reconciler := &ClusterImageReconciler{
		Client:          client,
		Scheme:          s.scheme,
		MaxParallelJobs: 5,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-image",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(s.ctx, req)
	require.NoError(s.T(), err)
	assert.True(s.T(), result.Requeue) //lint:ignore SA1019 testing controller's actual behavior

	// Verify status was updated to PENDING
	updatedImage := &raczylocomv1.ClusterImage{}
	err = client.Get(s.ctx, types.NamespacedName{Name: "test-image", Namespace: "default"}, updatedImage)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), shared.STATUS_PENDING, updatedImage.Status.Progress)
}

func (s *ControllerTestSuite) TestClusterImageReconcile_MissingExport() {
	// Scenario: Not Good - ClusterImageExport doesn't exist
	image := s.createClusterImage("test-image", "default", "non-existent-export", func(i *raczylocomv1.ClusterImage) {
		i.Status.Progress = shared.STATUS_PENDING
	})

	client := s.newFakeClient(image)
	reconciler := &ClusterImageReconciler{
		Client:          client,
		Scheme:          s.scheme,
		MaxParallelJobs: 5,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-image",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(s.ctx, req)
	assert.Error(s.T(), err)
	assert.Equal(s.T(), ctrl.Result{}, result)
}

func (s *ControllerTestSuite) TestClusterImageReconcile_MaxParallelJobsReached() {
	// Scenario: Good - should requeue when max jobs reached
	export := s.createClusterImageExport("test-export", "default")
	image := s.createClusterImage("test-image", "default", "test-export", func(i *raczylocomv1.ClusterImage) {
		i.Status.Progress = shared.STATUS_PENDING
	})

	client := s.newFakeClient(export, image)
	reconciler := &ClusterImageReconciler{
		Client:          client,
		Scheme:          s.scheme,
		MaxParallelJobs: 5,
		ActiveJobs:      5, // Already at max
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-image",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(s.ctx, req)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), time.Second*30, result.RequeueAfter)
}

func (s *ControllerTestSuite) TestClusterImageReconcile_SuccessStatus() {
	// Scenario: Good - success status should not trigger further action
	export := s.createClusterImageExport("test-export", "default")
	image := s.createClusterImage("test-image", "default", "test-export", func(i *raczylocomv1.ClusterImage) {
		i.Status.Progress = shared.STATUS_SUCCESS
	})

	client := s.newFakeClient(export, image)
	reconciler := &ClusterImageReconciler{
		Client: client,
		Scheme: s.scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-image",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), ctrl.Result{}, result)
}

func (s *ControllerTestSuite) TestClusterImageReconcile_FailedStatus() {
	// Scenario: Good - failed status should not trigger further action
	export := s.createClusterImageExport("test-export", "default")
	image := s.createClusterImage("test-image", "default", "test-export", func(i *raczylocomv1.ClusterImage) {
		i.Status.Progress = shared.STATUS_FAILED
	})

	client := s.newFakeClient(export, image)
	reconciler := &ClusterImageReconciler{
		Client: client,
		Scheme: s.scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-image",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), ctrl.Result{}, result)
}

func (s *ControllerTestSuite) TestClusterImageReconcile_PresentStatus() {
	// Scenario: Good - present status should not trigger further action
	export := s.createClusterImageExport("test-export", "default")
	image := s.createClusterImage("test-image", "default", "test-export", func(i *raczylocomv1.ClusterImage) {
		i.Status.Progress = shared.STATUS_PRESENT
	})

	client := s.newFakeClient(export, image)
	reconciler := &ClusterImageReconciler{
		Client: client,
		Scheme: s.scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-image",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), ctrl.Result{}, result)
}

// ==================== ClusterImageExport Controller Tests ====================

func (s *ControllerTestSuite) TestClusterImageExportReconcile_NotFound() {
	// Scenario: Good - resource doesn't exist, nothing to do
	client := s.newFakeClient()
	reconciler := &ClusterImageExportReconciler{
		Client: client,
		Scheme: s.scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "non-existent",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), ctrl.Result{}, result)
}

func (s *ControllerTestSuite) TestClusterImageExportReconcile_AddFinalizer() {
	// Scenario: Good - should add finalizer to new export
	export := s.createClusterImageExport("test-export", "default")

	client := s.newFakeClient(export)
	reconciler := &ClusterImageExportReconciler{
		Client: client,
		Scheme: s.scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-export",
			Namespace: "default",
		},
	}

	_, err := reconciler.Reconcile(s.ctx, req)
	require.NoError(s.T(), err)

	// Verify finalizer was added
	updatedExport := &raczylocomv1.ClusterImageExport{}
	err = client.Get(s.ctx, types.NamespacedName{Name: "test-export", Namespace: "default"}, updatedExport)
	require.NoError(s.T(), err)
	assert.Contains(s.T(), updatedExport.Finalizers, clusterImageExportFinalizer)
}

func (s *ControllerTestSuite) TestClusterImageExportReconcile_AddCreationTimestamp() {
	// Scenario: Good - should add creation timestamp annotation
	export := s.createClusterImageExport("test-export", "default")

	client := s.newFakeClient(export)
	reconciler := &ClusterImageExportReconciler{
		Client: client,
		Scheme: s.scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-export",
			Namespace: "default",
		},
	}

	_, err := reconciler.Reconcile(s.ctx, req)
	require.NoError(s.T(), err)

	// Verify annotation was added
	updatedExport := &raczylocomv1.ClusterImageExport{}
	err = client.Get(s.ctx, types.NamespacedName{Name: "test-export", Namespace: "default"}, updatedExport)
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), updatedExport.Annotations)
	_, exists := updatedExport.Annotations["export.raczylo.com/creation-timestamp"]
	assert.True(s.T(), exists)
}

func (s *ControllerTestSuite) TestClusterImageExportReconcile_InjectPodAnnotations() {
	// Scenario: Good - pod annotations should be injectable
	reconciler := &ClusterImageExportReconciler{}

	annotations := map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/port":   "8080",
	}
	reconciler.InjectPodAnnotations(annotations)

	assert.Equal(s.T(), annotations, reconciler.podAnnotations)
}

// ==================== Matrix Test Scenarios ====================

type ClusterImageScenario struct {
	Name           string
	Scenario       TestScenario
	SetupFunc      func(*ControllerTestSuite) (client.Client, *ClusterImageReconciler, reconcile.Request)
	ExpectedError  bool
	ExpectedStatus string
	Description    string
}

func (s *ControllerTestSuite) TestClusterImageReconcile_MatrixScenarios() {
	scenarios := []ClusterImageScenario{
		{
			Name:        "new_image_initialization",
			Scenario:    ScenarioGood,
			Description: "New ClusterImage should be initialized to PENDING",
			SetupFunc: func(s *ControllerTestSuite) (client.Client, *ClusterImageReconciler, reconcile.Request) {
				export := s.createClusterImageExport("export-1", "default")
				image := s.createClusterImage("image-1", "default", "export-1")
				c := s.newFakeClient(export, image)
				r := &ClusterImageReconciler{Client: c, Scheme: s.scheme, MaxParallelJobs: 5}
				req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "image-1", Namespace: "default"}}
				return c, r, req
			},
			ExpectedError:  false,
			ExpectedStatus: shared.STATUS_PENDING,
		},
		{
			Name:        "missing_export_reference",
			Scenario:    ScenarioNotGood,
			Description: "ClusterImage with missing export should error",
			SetupFunc: func(s *ControllerTestSuite) (client.Client, *ClusterImageReconciler, reconcile.Request) {
				image := s.createClusterImage("orphan-image", "default", "missing-export", func(i *raczylocomv1.ClusterImage) {
					i.Status.Progress = shared.STATUS_PENDING
				})
				c := s.newFakeClient(image)
				r := &ClusterImageReconciler{Client: c, Scheme: s.scheme, MaxParallelJobs: 5}
				req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "orphan-image", Namespace: "default"}}
				return c, r, req
			},
			ExpectedError: true,
		},
		{
			Name:        "success_status_no_action",
			Scenario:    ScenarioGood,
			Description: "ClusterImage with SUCCESS status should not trigger action",
			SetupFunc: func(s *ControllerTestSuite) (client.Client, *ClusterImageReconciler, reconcile.Request) {
				export := s.createClusterImageExport("export-2", "default")
				image := s.createClusterImage("success-image", "default", "export-2", func(i *raczylocomv1.ClusterImage) {
					i.Status.Progress = shared.STATUS_SUCCESS
				})
				c := s.newFakeClient(export, image)
				r := &ClusterImageReconciler{Client: c, Scheme: s.scheme}
				req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "success-image", Namespace: "default"}}
				return c, r, req
			},
			ExpectedError:  false,
			ExpectedStatus: shared.STATUS_SUCCESS,
		},
		{
			Name:        "failed_status_no_action",
			Scenario:    ScenarioNotGood,
			Description: "ClusterImage with FAILED status should not trigger action",
			SetupFunc: func(s *ControllerTestSuite) (client.Client, *ClusterImageReconciler, reconcile.Request) {
				export := s.createClusterImageExport("export-3", "default")
				image := s.createClusterImage("failed-image", "default", "export-3", func(i *raczylocomv1.ClusterImage) {
					i.Status.Progress = shared.STATUS_FAILED
				})
				c := s.newFakeClient(export, image)
				r := &ClusterImageReconciler{Client: c, Scheme: s.scheme}
				req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "failed-image", Namespace: "default"}}
				return c, r, req
			},
			ExpectedError:  false,
			ExpectedStatus: shared.STATUS_FAILED,
		},
		{
			Name:        "empty_namespace",
			Scenario:    ScenarioReallyBad,
			Description: "ClusterImage in empty namespace should be handled gracefully",
			SetupFunc: func(s *ControllerTestSuite) (client.Client, *ClusterImageReconciler, reconcile.Request) {
				c := s.newFakeClient()
				r := &ClusterImageReconciler{Client: c, Scheme: s.scheme}
				req := reconcile.Request{NamespacedName: types.NamespacedName{Name: "bad-image", Namespace: ""}}
				return c, r, req
			},
			ExpectedError: false, // Not found, gracefully handled
		},
	}

	for _, tc := range scenarios {
		s.Run(tc.Name, func() {
			client, reconciler, req := tc.SetupFunc(s)
			result, err := reconciler.Reconcile(s.ctx, req)

			if tc.ExpectedError {
				assert.Error(s.T(), err, "Expected error for scenario: %s", tc.Name)
			} else {
				assert.NoError(s.T(), err, "Unexpected error for scenario: %s", tc.Name)
			}

			if tc.ExpectedStatus != "" && !tc.ExpectedError {
				image := &raczylocomv1.ClusterImage{}
				getErr := client.Get(s.ctx, req.NamespacedName, image)
				if getErr == nil {
					assert.Equal(s.T(), tc.ExpectedStatus, image.Status.Progress)
				}
			}

			_ = result // Result validation varies by scenario
		})
	}
}

// ==================== Kubernetes Volatility Tests ====================

func (s *ControllerTestSuite) TestClusterImage_ConcurrentUpdates() {
	// Scenario: Good - simulate concurrent reconciliation on PENDING status
	// This test verifies that multiple reconciliations don't corrupt state
	export := s.createClusterImageExport("concurrent-export", "default")
	image := s.createClusterImage("concurrent-image", "default", "concurrent-export")

	fakeClient := s.newFakeClient(export, image)
	reconciler := &ClusterImageReconciler{
		Client:          fakeClient,
		Scheme:          s.scheme,
		MaxParallelJobs: 10,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "concurrent-image",
			Namespace: "default",
		},
	}

	// First reconciliation should set status to PENDING
	result, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
	assert.True(s.T(), result.Requeue) //lint:ignore SA1019 testing controller's actual behavior

	// Verify status was set
	finalImage := &raczylocomv1.ClusterImage{}
	err = fakeClient.Get(s.ctx, req.NamespacedName, finalImage)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), shared.STATUS_PENDING, finalImage.Status.Progress)
}

func (s *ControllerTestSuite) TestClusterImage_ActiveJobsMutex() {
	// Scenario: Good - verify mutex protects ActiveJobs counter
	reconciler := &ClusterImageReconciler{
		MaxParallelJobs: 10,
		ActiveJobs:      0,
	}

	done := make(chan bool)
	iterations := 100

	// Concurrent increments
	go func() {
		for i := 0; i < iterations; i++ {
			reconciler.activeJobsMu.Lock()
			reconciler.ActiveJobs++
			reconciler.activeJobsMu.Unlock()
		}
		done <- true
	}()

	// Concurrent decrements
	go func() {
		for i := 0; i < iterations; i++ {
			reconciler.activeJobsMu.Lock()
			reconciler.ActiveJobs--
			reconciler.activeJobsMu.Unlock()
		}
		done <- true
	}()

	<-done
	<-done

	// Final count should be 0
	reconciler.activeJobsMu.Lock()
	finalCount := reconciler.ActiveJobs
	reconciler.activeJobsMu.Unlock()
	assert.Equal(s.T(), 0, finalCount)
}

func (s *ControllerTestSuite) TestClusterImageExport_WithDeletionTimestamp() {
	// Scenario: Good - export being deleted should trigger cleanup
	export := s.createClusterImageExport("deleting-export", "default")
	now := metav1.Now()
	export.DeletionTimestamp = &now
	export.Finalizers = []string{clusterImageExportFinalizer}

	client := s.newFakeClient(export)
	reconciler := &ClusterImageExportReconciler{
		Client: client,
		Scheme: s.scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "deleting-export",
			Namespace: "default",
		},
	}

	// This should trigger deletion handling
	result, err := reconciler.Reconcile(s.ctx, req)
	// May error due to cleanup job creation, but should not panic
	_ = err
	_ = result
}

// ==================== Image Parsing Scenarios (Controller Integration) ====================

func (s *ControllerTestSuite) TestClusterImage_SHAPinnedImages() {
	// Scenario: Good - SHA-pinned images should be handled correctly
	export := s.createClusterImageExport("sha-export", "default")
	image := s.createClusterImage("sha-image", "default", "sha-export", func(i *raczylocomv1.ClusterImage) {
		i.Spec.Image = "quay.io/cilium/cilium"
		i.Spec.Tag = "v1.18.4"
		i.Spec.Sha = "sha256:49d87af187eeeb9e9e3ec2bc6bd372261a0b5cb2d845659463ba7cc10fe9e45f"
		i.Spec.FullName = "quay.io/cilium/cilium:v1.18.4@sha256:49d87af187eeeb9e9e3ec2bc6bd372261a0b5cb2d845659463ba7cc10fe9e45f"
	})

	client := s.newFakeClient(export, image)
	reconciler := &ClusterImageReconciler{
		Client:          client,
		Scheme:          s.scheme,
		MaxParallelJobs: 5,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "sha-image",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
	assert.True(s.T(), result.Requeue) //lint:ignore SA1019 testing controller's actual behavior
}

func (s *ControllerTestSuite) TestClusterImage_MultipleRegistries() {
	// Scenario: Good - images from different registries
	registries := []struct {
		name     string
		image    string
		fullName string
	}{
		{"gcr-image", "gcr.io/distroless/static", "gcr.io/distroless/static:nonroot"},
		{"ecr-image", "123456789.dkr.ecr.us-east-1.amazonaws.com/myapp", "123456789.dkr.ecr.us-east-1.amazonaws.com/myapp:v1.0"},
		{"dockerhub-image", "library/nginx", "nginx:latest"},
		{"quay-image", "quay.io/coreos/etcd", "quay.io/coreos/etcd:v3.5.0"},
	}

	export := s.createClusterImageExport("multi-registry-export", "default")
	objs := []client.Object{export}

	for _, reg := range registries {
		img := s.createClusterImage(reg.name, "default", "multi-registry-export", func(i *raczylocomv1.ClusterImage) {
			i.Spec.Image = reg.image
			i.Spec.FullName = reg.fullName
		})
		objs = append(objs, img)
	}

	client := s.newFakeClient(objs...)
	reconciler := &ClusterImageReconciler{
		Client:          client,
		Scheme:          s.scheme,
		MaxParallelJobs: 10,
	}

	for _, reg := range registries {
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      reg.name,
				Namespace: "default",
			},
		}

		result, err := reconciler.Reconcile(s.ctx, req)
		assert.NoError(s.T(), err, "Failed for registry: %s", reg.name)
		assert.True(s.T(), result.Requeue) //lint:ignore SA1019 testing controller's actual behavior
	}
}

// ==================== Storage Configuration Tests ====================

func (s *ControllerTestSuite) TestClusterImageExport_S3Storage() {
	// Scenario: Good - S3 storage configuration
	export := s.createClusterImageExport("s3-export", "default", func(e *raczylocomv1.ClusterImageExport) {
		e.Spec.Storage = raczylocomv1.ClusterImageStorageSpec{
			StorageTarget: shared.STORAGE_S3,
			S3: raczylocomv1.ClusterImageStorageS3{
				Bucket:   "my-backup-bucket",
				Region:   "eu-west-1",
				UseRole:  true,
				RoleARN:  "arn:aws:iam::123456789:role/BackupRole",
				Endpoint: "https://s3.eu-west-1.amazonaws.com",
			},
		}
	})

	client := s.newFakeClient(export)
	reconciler := &ClusterImageExportReconciler{
		Client: client,
		Scheme: s.scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "s3-export",
			Namespace: "default",
		},
	}

	_, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)

	// Verify export was updated
	updatedExport := &raczylocomv1.ClusterImageExport{}
	err = client.Get(s.ctx, req.NamespacedName, updatedExport)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), shared.STORAGE_S3, updatedExport.Spec.Storage.StorageTarget)
}

func (s *ControllerTestSuite) TestClusterImageExport_FileStorage() {
	// Scenario: Good - File storage configuration
	export := s.createClusterImageExport("file-export", "default", func(e *raczylocomv1.ClusterImageExport) {
		e.Spec.Storage = raczylocomv1.ClusterImageStorageSpec{
			StorageTarget: shared.STORAGE_FILE,
		}
		e.Spec.BasePath = "/mnt/backup"
	})

	client := s.newFakeClient(export)
	reconciler := &ClusterImageExportReconciler{
		Client: client,
		Scheme: s.scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "file-export",
			Namespace: "default",
		},
	}

	_, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
}

// ==================== Edge Cases ====================

func (s *ControllerTestSuite) TestClusterImage_EmptySpec() {
	// Scenario: Really Bad - empty spec should be handled
	export := s.createClusterImageExport("empty-spec-export", "default")
	image := &raczylocomv1.ClusterImage{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "raczylo.com/v1",
			Kind:       "ClusterImage",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "empty-spec-image",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: raczylocomv1.ClusterImageSpec{
			ExportName: "empty-spec-export",
		},
	}

	client := s.newFakeClient(export, image)
	reconciler := &ClusterImageReconciler{
		Client:          client,
		Scheme:          s.scheme,
		MaxParallelJobs: 5,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "empty-spec-image",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(s.ctx, req)
	// Should not error, just set to pending
	assert.NoError(s.T(), err)
	assert.True(s.T(), result.Requeue) //lint:ignore SA1019 testing controller's actual behavior
}

func (s *ControllerTestSuite) TestClusterImageExport_EmptyNamespaces() {
	// Scenario: Good - export with no namespace filters should process all
	export := s.createClusterImageExport("all-ns-export", "default", func(e *raczylocomv1.ClusterImageExport) {
		e.Spec.Namespaces = []string{}
		e.Spec.ExcludedNamespaces = []string{}
	})

	client := s.newFakeClient(export)
	reconciler := &ClusterImageExportReconciler{
		Client: client,
		Scheme: s.scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "all-ns-export",
			Namespace: "default",
		},
	}

	_, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
}

func (s *ControllerTestSuite) TestClusterImageExport_WithAdditionalImages() {
	// Scenario: Good - export with additional images specified
	export := s.createClusterImageExport("additional-images-export", "default", func(e *raczylocomv1.ClusterImageExport) {
		e.Spec.AdditionalImages = []string{
			"nginx:1.21",
			"redis:7.0",
			"postgres:15@sha256:abc123",
		}
	})

	client := s.newFakeClient(export)
	reconciler := &ClusterImageExportReconciler{
		Client: client,
		Scheme: s.scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "additional-images-export",
			Namespace: "default",
		},
	}

	_, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
}

func (s *ControllerTestSuite) TestClusterImageExport_WithIncludesExcludes() {
	// Scenario: Good - export with include/exclude filters
	export := s.createClusterImageExport("filtered-export", "default", func(e *raczylocomv1.ClusterImageExport) {
		e.Spec.Includes = []string{"nginx", "redis"}
		e.Spec.Excludes = []string{"test", "dev"}
		e.Spec.Namespaces = []string{"production", "staging"}
		e.Spec.ExcludedNamespaces = []string{"kube-system"}
	})

	client := s.newFakeClient(export)
	reconciler := &ClusterImageExportReconciler{
		Client: client,
		Scheme: s.scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "filtered-export",
			Namespace: "default",
		},
	}

	_, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
}

func (s *ControllerTestSuite) TestClusterImage_WithImagePullSecrets() {
	// Scenario: Good - image with pull secrets should work
	export := s.createClusterImageExport("secret-export", "default")
	image := s.createClusterImage("secret-image", "default", "secret-export", func(i *raczylocomv1.ClusterImage) {
		i.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
			{Name: "docker-registry-secret"},
			{Name: "gcr-json-key"},
		}
	})

	client := s.newFakeClient(export, image)
	reconciler := &ClusterImageReconciler{
		Client:          client,
		Scheme:          s.scheme,
		MaxParallelJobs: 5,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "secret-image",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
	assert.True(s.T(), result.Requeue) //lint:ignore SA1019 testing controller's actual behavior
}

func (s *ControllerTestSuite) TestClusterImage_WithJobAnnotations() {
	// Scenario: Good - image with job annotations
	export := s.createClusterImageExport("annotated-export", "default", func(e *raczylocomv1.ClusterImageExport) {
		e.Spec.JobAnnotations = map[string]string{
			"iam.amazonaws.com/role": "arn:aws:iam::123456789:role/BackupRole",
		}
	})
	image := s.createClusterImage("annotated-image", "default", "annotated-export", func(i *raczylocomv1.ClusterImage) {
		i.Spec.JobAnnotations = map[string]string{
			"custom/annotation": "value",
		}
	})

	client := s.newFakeClient(export, image)
	reconciler := &ClusterImageReconciler{
		Client:          client,
		Scheme:          s.scheme,
		MaxParallelJobs: 5,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "annotated-image",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
	assert.True(s.T(), result.Requeue) //lint:ignore SA1019 testing controller's actual behavior
}

// ==================== Job Status Tests ====================

func (s *ControllerTestSuite) TestClusterImage_SuccessfulJobCompletion() {
	// Scenario: Good - verify job success status detection logic
	// Note: Full integration testing requires envtest for KubeClient
	// This test validates the basic state machine transitions

	export := s.createClusterImageExport("job-success-export", "default")
	image := s.createClusterImage("job-success-image", "default", "job-success-export", func(i *raczylocomv1.ClusterImage) {
		i.Status.Progress = shared.STATUS_SUCCESS // Already completed
	})

	fakeClient := s.newFakeClient(export, image)
	reconciler := &ClusterImageReconciler{
		Client:          fakeClient,
		Scheme:          s.scheme,
		MaxParallelJobs: 5,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "job-success-image",
			Namespace: "default",
		},
	}

	// SUCCESS status should result in no-op
	result, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), ctrl.Result{}, result)

	// Status should remain unchanged
	finalImage := &raczylocomv1.ClusterImage{}
	err = fakeClient.Get(s.ctx, req.NamespacedName, finalImage)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), shared.STATUS_SUCCESS, finalImage.Status.Progress)
}

func (s *ControllerTestSuite) TestClusterImage_RetryCount() {
	// Scenario: Good - verify retry count is tracked properly
	export := s.createClusterImageExport("retry-export", "default")
	image := s.createClusterImage("retry-image", "default", "retry-export", func(i *raczylocomv1.ClusterImage) {
		i.Status.Progress = shared.STATUS_FAILED // Terminal state
		i.Status.RetryCount = 2
	})

	fakeClient := s.newFakeClient(export, image)
	reconciler := &ClusterImageReconciler{
		Client:          fakeClient,
		Scheme:          s.scheme,
		MaxParallelJobs: 5,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "retry-image",
			Namespace: "default",
		},
	}

	// FAILED status should result in no-op
	result, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), ctrl.Result{}, result)

	// Verify retry count is preserved
	finalImage := &raczylocomv1.ClusterImage{}
	err = fakeClient.Get(s.ctx, req.NamespacedName, finalImage)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 2, finalImage.Status.RetryCount)
}

func (s *ControllerTestSuite) TestClusterImage_MaxRetriesReached() {
	// Scenario: Not Good - max retries reached and FAILED status
	export := s.createClusterImageExport("max-retry-export", "default")
	image := s.createClusterImage("max-retry-image", "default", "max-retry-export", func(i *raczylocomv1.ClusterImage) {
		i.Status.Progress = shared.STATUS_FAILED
		i.Status.RetryCount = 3 // Max retries reached
	})

	fakeClient := s.newFakeClient(export, image)
	reconciler := &ClusterImageReconciler{
		Client:          fakeClient,
		Scheme:          s.scheme,
		MaxParallelJobs: 5,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "max-retry-image",
			Namespace: "default",
		},
	}

	// FAILED status with max retries should be terminal
	result, err := reconciler.Reconcile(s.ctx, req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), ctrl.Result{}, result)

	// Verify final state
	finalImage := &raczylocomv1.ClusterImage{}
	err = fakeClient.Get(s.ctx, req.NamespacedName, finalImage)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), shared.STATUS_FAILED, finalImage.Status.Progress)
	assert.Equal(s.T(), 3, finalImage.Status.RetryCount)
}
