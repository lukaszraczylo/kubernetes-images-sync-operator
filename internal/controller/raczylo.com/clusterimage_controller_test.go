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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	raczylocomv1 "github.com/lukaszraczylo/kubernetes-images-sync-operator/api/raczylo.com/v1"
)

var _ = Describe("ClusterImage Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const exportName = "test-export"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		exportNamespacedName := types.NamespacedName{
			Name:      exportName,
			Namespace: "default",
		}
		clusterimage := &raczylocomv1.ClusterImage{}

		BeforeEach(func() {
			By("creating the ClusterImageExport that the ClusterImage references")
			export := &raczylocomv1.ClusterImageExport{}
			err := k8sClient.Get(ctx, exportNamespacedName, export)
			if err != nil && errors.IsNotFound(err) {
				exportResource := &raczylocomv1.ClusterImageExport{
					ObjectMeta: metav1.ObjectMeta{
						Name:      exportName,
						Namespace: "default",
					},
					Spec: raczylocomv1.ClusterImageExportSpec{
						Name:              exportName,
						BasePath:          "/backups/test",
						MaxConcurrentJobs: 1,
						Storage: raczylocomv1.ClusterImageStorageSpec{
							StorageTarget: "FILE",
						},
					},
				}
				Expect(k8sClient.Create(ctx, exportResource)).To(Succeed())
			}

			By("creating the custom resource for the Kind ClusterImage")
			err = k8sClient.Get(ctx, typeNamespacedName, clusterimage)
			if err != nil && errors.IsNotFound(err) {
				resource := &raczylocomv1.ClusterImage{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: raczylocomv1.ClusterImageSpec{
						ExportName: exportName,
						Image:      "nginx:latest",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance ClusterImage")
			resource := &raczylocomv1.ClusterImage{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}

			By("Cleanup the ClusterImageExport")
			export := &raczylocomv1.ClusterImageExport{}
			err = k8sClient.Get(ctx, exportNamespacedName, export)
			if err == nil {
				Expect(k8sClient.Delete(ctx, export)).To(Succeed())
			}
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ClusterImageReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})
