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

// RestoreSpec defines the desired state of Restore
type RestoreSpec struct {
	ApplicationName string `json:"applicationName,omitempty"` // Name of the application to restore
	BackupFile      string `json:"backupFile,omitempty"`      // Backup file to restore
	StorageLocation string `json:"storageLocation,omitempty"` // S3/MinIO bucket location
}

// RestoreStatus defines the observed state of Restore
type RestoreStatus struct {
	RestoreState    string       `json:"restoreState,omitempty"`    // InProgress, Completed, Failed
	LastRestoreTime *metav1.Time `json:"lastRestoreTime,omitempty"` // Timestamp of the last successful restore
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Restore is the Schema for the restores API
type Restore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RestoreSpec   `json:"spec,omitempty"`
	Status RestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RestoreList contains a list of Restore
type RestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Restore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Restore{}, &RestoreList{})
}
