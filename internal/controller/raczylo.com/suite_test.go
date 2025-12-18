/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package raczylocom

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	raczylocomv1 "github.com/lukaszraczylo/kubernetes-images-sync-operator/api/raczylo.com/v1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var ctx context.Context
var cancel context.CancelFunc

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")

	// Use KUBEBUILDER_ASSETS if set (CI), otherwise fall back to local path
	binaryAssetsDir := os.Getenv("KUBEBUILDER_ASSETS")
	if binaryAssetsDir == "" {
		binaryAssetsDir = filepath.Join("..", "..", "..", "bin", "k8s",
			fmt.Sprintf("1.31.0-%s-%s", runtime.GOOS, runtime.GOARCH))
	}

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: binaryAssetsDir,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = raczylocomv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	// Create a manager to get a cache-backed client that supports field selectors
	// Explicitly configure cache to watch ClusterImage and ClusterImageExport resources
	// This is required for field selectors to work in tests (without registering controllers)
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&raczylocomv1.ClusterImage{}:       {},
				&raczylocomv1.ClusterImageExport{}: {},
			},
		},
	})
	Expect(err).NotTo(HaveOccurred())

	// Add field index for spec.exportName on ClusterImage
	err = mgr.GetFieldIndexer().IndexField(ctx, &raczylocomv1.ClusterImage{}, "spec.exportName", func(obj client.Object) []string {
		clusterImage := obj.(*raczylocomv1.ClusterImage)
		return []string{clusterImage.Spec.ExportName}
	})
	Expect(err).NotTo(HaveOccurred())

	// Start the manager cache in background
	go func() {
		defer GinkgoRecover()
		err := mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()

	// Wait for cache to sync
	Expect(mgr.GetCache().WaitForCacheSync(ctx)).To(BeTrue())

	k8sClient = mgr.GetClient()
	Expect(k8sClient).NotTo(BeNil())

})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
