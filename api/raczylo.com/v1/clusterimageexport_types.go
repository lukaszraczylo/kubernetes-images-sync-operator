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

type ClusterImageStorageS3 struct {
	// Bucket name
	Bucket string `json:"bucket"`
	Region string `json:"region"`
	// S3 bucket credentials
	AccessKey string `json:"accessKey,omitempty"`
	SecretKey string `json:"secretKey,omitempty"`
	UseRole   bool   `json:"useRole,omitempty"`
	// RoleARN is the ARN of the role to be used for the deployment
	RoleARN string `json:"roleARN,omitempty"`
	// Defines the endpoint for the S3 storage
	// If none specified - default AWS endpoint will be used
	Endpoint string `json:"endpoint,omitempty"`
	// Defines the secret name for credentials
	SecretName string `json:"secretName,omitempty"`
}

// ClusterImageStorageSpec defines the desired state of ClusterImageStorage
type ClusterImageStorageSpec struct {
	// +kubebuilder:validation:Enum=file;S3
	StorageTarget string                `json:"target"`
	S3            ClusterImageStorageS3 `json:"s3,omitempty"`
}

// ClusterImageExportSpec defines the desired state of ClusterImageExport
// +kubebuilder:printcolumn:name="BasePath",type="string",JSONPath=".spec.basePath"
// +kubebuilder:printcolumn:name="Storage",type="string",JSONPath=".spec.storage.target"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type ClusterImageExportSpec struct {
	Name      string      `json:"name"`
	CreatedAt metav1.Time `json:"createdAt,omitempty"`
	// Exclude images which contain these strings
	Excludes []string `json:"excludes,omitempty"`
	// Include only images which contain these strings
	Includes []string `json:"includes,omitempty"`
	// Base path for the export - both file and S3
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	BasePath string                  `json:"basePath"`
	Storage  ClusterImageStorageSpec `json:"storage"`
	// +kubebuilder:validation.Minimum=1
	// +kubebuilder:validation.Maximum=100
	MaxConcurrentJobs int `json:"maxConcurrentJobs"`
}

// ClusterImageExportStatus defines the observed state of ClusterImageExport
type ClusterImageExportStatus struct {
	Progress string `json:"progress,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ClusterImageExport is the Schema for the clusterimageexports API
// +kubebuilder:printcolumn:name="BasePath",type="string",JSONPath=".spec.basePath"
// +kubebuilder:printcolumn:name="Storage",type="string",JSONPath=".spec.storage.target"
// +kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.progress"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type ClusterImageExport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterImageExportSpec   `json:"spec,omitempty"`
	Status ClusterImageExportStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterImageExportList contains a list of ClusterImageExport
type ClusterImageExportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterImageExport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterImageExport{}, &ClusterImageExportList{})
}
