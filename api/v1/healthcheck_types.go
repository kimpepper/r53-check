/*

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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// HealthCheckSpec defines the desired state of HealthCheck
type HealthCheckSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	NamePrefix    string   `json:"name_prefix,omitempty"`
	Domain        string   `json:"domain,omitempty"`
	Type          string   `json:"type,omitempty"`
	Port          int64    `json:"port,omitempty"`
	ResourcePath  string   `json:"resource_path,omitempty"`
	Disabled      bool     `json:"disabled,omitempty"`
	AlarmDisabled bool     `json:"alarm_disabled,omitempty"`
	AlarmActions  []string `json:"alarm_actions,omitempty"`
	OKActions     []string `json:"ok_actions,omitempty"`
}

// HealthCheckStatus defines the observed state of HealthCheck
type HealthCheckStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	HealthCheckId string `json:"id,omitempty"`
	AlarmName     string `json:"alarm_name,omitempty"`
	AlarmState    string `json:"alarm_state,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// HealthCheck is the Schema for the healthchecks API
type HealthCheck struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HealthCheckSpec   `json:"spec,omitempty"`
	Status HealthCheckStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HealthCheckList contains a list of HealthCheck
type HealthCheckList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HealthCheck `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HealthCheck{}, &HealthCheckList{})
}
