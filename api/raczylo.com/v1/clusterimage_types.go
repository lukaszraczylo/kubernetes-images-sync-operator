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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterImageSpec defines the desired state of ClusterImage
// +kubebuilder:printcolumn:name="Image",type="string",JSONPath=".spec.image"
// +kubebuilder:printcolumn:name="Tag",type="string",JSONPath=".spec.tag"
// +kubebuilder:printcolumn:name="SHA",type="string",JSONPath=".spec.sha"
// +kubebuilder:printcolumn:name="Storage",type="string",JSONPath=".spec.storage"
// +kubebuilder:printcolumn:name="Path",type="string",JSONPath=".spec.exportPath"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type ClusterImageSpec struct {
	Image          string            `json:"image,omitempty"`
	Tag            string            `json:"tag,omitempty"`
	Sha            string            `json:"sha,omitempty"`
	FullName       string            `json:"fullName,omitempty"` // Because I'm lazy and it's easier to pull that way
	Storage        string            `json:"storage,omitempty"`
	ExportName     string            `json:"exportName"`
	ExportPath     string            `json:"exportPath,omitempty"`
	ImageNamespace string            `json:"imageNamespace,omitempty"`
	JobAnnotations map[string]string `json:"jobAnnotations,omitempty"`
}

// ClusterImageStatus defines the observed state of ClusterImage
type ClusterImageStatus struct {
	Progress string `json:"progress,omitempty"`
	// default value is 0
	// +kubebuilder:default:=0
	RetryCount int `json:"retryCount,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// ClusterImage is the Schema for the clusterimages API
// +kubebuilder:printcolumn:name="Ref",type="string",JSONPath=".spec.exportName"
// +kubebuilder:printcolumn:name="Image",type="string",JSONPath=".spec.image"
// +kubebuilder:printcolumn:name="Tag",type="string",JSONPath=".spec.tag"
// +kubebuilder:printcolumn:name="SHA",type="string",JSONPath=".spec.sha"
// +kubebuilder:printcolumn:name="Storage",type="string",JSONPath=".spec.storage"
// +kubebuilder:printcolumn:name="Path",type="string",JSONPath=".spec.exportPath"
// +kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.progress"
// +kubebuilder:printcolumn:name="Retries",type="integer",JSONPath=".status.retryCount"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type ClusterImage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterImageSpec   `json:"spec,omitempty"`
	Status ClusterImageStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterImageList contains a list of ClusterImage
type ClusterImageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterImage `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterImage{}, &ClusterImageList{})
}
