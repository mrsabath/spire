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

package controllers

import (
	"fmt"
	"testing"

	spiffeidv1beta1 "github.com/spiffe/spire/support/k8s/k8s-workload-registrar/mode-crd/api/spiffeid/v1beta1"
	"github.com/stretchr/testify/suite"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	PodName           string = "test-pod"
	PodNamespace      string = "default"
	PodServiceAccount string = "serviceAccount"
)

func TestPodController(t *testing.T) {
	suite.Run(t, new(PodControllerTestSuite))
}

type PodControllerTestSuite struct {
	suite.Suite
	CommonControllerTestSuite
}

func (s *PodControllerTestSuite) SetupSuite() {
	s.CommonControllerTestSuite = NewCommonControllerTestSuite(s.T())
}

// TestPodLabel adds a label to a pod and check if the SPIFFE ID is generated correctly.
// It then updates the label and ensures the SPIFFE ID is updated.
func (s *PodControllerTestSuite) TestPodLabel() {
	tests := []struct {
		PodLabel         string
		PodAnnotation    string
		first            string
		second           string
		identityTemplate string
		context          map[string]string
		uid              string
		err              string
	}{
		{
			PodLabel: "spiffe",
			first:    "test-label",
			second:   "new-test-label",
		},
		{
			PodAnnotation: "spiffe",
			first:         "test-annotation",
			second:        "new-test-annotation",
		},
		{
			// Default format, without the template
			first:  fmt.Sprintf("ns/%s/sa/%s", PodNamespace, PodServiceAccount),
			second: fmt.Sprintf("ns/%s/sa/%s", PodNamespace, PodServiceAccount),
		},
		{
			// Default format, template provided
			identityTemplate: "ns/{{.Pod.namespace}}/sa/{{.Pod.service_account}}/podName/{{.Pod.pod_name}}",
			first:            fmt.Sprintf("ns/%s/sa/%s/podName/%s", PodNamespace, PodServiceAccount, PodName),
			second:           fmt.Sprintf("ns/%s/sa/%s/podName/%s", PodNamespace, PodServiceAccount, PodName),
		},
		{
			// Testing provided identity template, corresponding to a default format:
			identityTemplate: "ns/{{.Pod.namespace}}/sa/{{.Pod.service_account}}",
			first:            "ns/" + PodNamespace + "/sa/" + PodServiceAccount,
			second:           "ns/" + PodNamespace + "/sa/" + PodServiceAccount,
		},
		{
			// Testing provided identity template:
			identityTemplate: "ns/{{.Pod.namespace}}/sa/{{.Pod.service_account}}/podName/{{.Pod.pod_name}}",
			first:            "ns/" + PodNamespace + "/sa/" + PodServiceAccount + "/podName/" + PodName,
			second:           "ns/" + PodNamespace + "/sa/" + PodServiceAccount + "/podName/" + PodName,
		},
		{
			// Testing provided identity template with an additional identity context:
			context: map[string]string{
				"region":       "EU-DE",
				"cluster_name": "MYCLUSTER",
			},
			identityTemplate: "region/{{.Context.region}}/cluster/{{.Context.cluster_name}}/podName/{{.Pod.pod_name}}",
			first:            "region/EU-DE/cluster/MYCLUSTER/podName/" + PodName,
			second:           "region/EU-DE/cluster/MYCLUSTER/podName/" + PodName,
		},
		{
			// Testing identity template with other Pod arguments:
			identityTemplate: fmt.Sprintf("{{.Pod.%s}}/{{.Pod.%s}}/{{.Pod.%s}}/{{.Pod.%s}}/{{.Pod.%s}}", PodNameIDLabel, NamespaceIDLabel, PodServiceAccountIDLabel, PodHostnameLabel, PodNodeNameLabel),
			first:            PodName + "/" + PodNamespace + "/" + PodServiceAccount + "/hostname/test-node",
			second:           PodName + "/" + PodNamespace + "/" + PodServiceAccount + "/hostname/test-node",
		},
		{
			// Testing invalid identity template:
			identityTemplate: "invalid/",
			err:              "invalid SVID, ends with /",
		},
		{
			// Testing identity template with a missing context value:
			identityTemplate: "region/{{.Context.region}}",
			err:              "template refrences a value not included in context map",
		},
	}

	for _, test := range tests {
		p := NewPodReconciler(PodReconcilerConfig{
			Client:           s.k8sClient,
			Cluster:          s.cluster,
			Ctx:              s.ctx,
			Log:              s.log,
			PodLabel:         test.PodLabel,
			PodAnnotation:    test.PodAnnotation,
			Scheme:           s.scheme,
			TrustDomain:      s.trustDomain,
			IdentityTemplate: test.identityTemplate,
			Context:          test.context,
		})

		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        PodName,
				Namespace:   PodNamespace,
				Labels:      map[string]string{"spiffe": test.first},
				Annotations: map[string]string{"spiffe": test.first},
				UID:         types.UID(test.uid),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "test-pod",
					Image: "test-pod",
				}},
				NodeName:           "test-node",
				Hostname:           "hostname",
				ServiceAccountName: "serviceAccount",
			},
		}
		err := s.k8sClient.Create(s.ctx, &pod)
		s.Require().NoError(err)
		err = s.reconcile(p)
		if err != nil {
			s.Require().Error(err)
			s.Require().Contains(err.Error(), test.err)
			err = s.k8sClient.Delete(s.ctx, &pod)
			s.Require().NoError(err)
			continue
		}

		labelSelector := labels.Set(map[string]string{
			"podUid": string(pod.ObjectMeta.UID),
		})

		// Verify that exactly 1 SPIFFE ID  resource was created for this pod
		spiffeIDList := spiffeidv1beta1.SpiffeIDList{}
		err = s.k8sClient.List(s.ctx, &spiffeIDList, &client.ListOptions{
			LabelSelector: labelSelector.AsSelector(),
		})
		s.Require().NoError(err)
		s.Require().Len(spiffeIDList.Items, 1)

		// Verify the label/annotation matches what we expect
		expectedSpiffeID := makeID(s.trustDomain, "%s", test.first)
		actualSpiffeID := spiffeIDList.Items[0].Spec.SpiffeId
		s.Require().Equal(expectedSpiffeID, actualSpiffeID)

		// Update the labels/annotations
		pod.Labels["spiffe"] = test.second
		pod.Annotations["spiffe"] = test.second
		err = s.k8sClient.Update(s.ctx, &pod)
		s.Require().NoError(err)
		s.reconcile(p)

		// Verify that there is still exactly 1 SPIFFE ID resource for this pod
		spiffeIDList = spiffeidv1beta1.SpiffeIDList{}
		err = s.k8sClient.List(s.ctx, &spiffeIDList, &client.ListOptions{
			LabelSelector: labelSelector.AsSelector(),
		})
		s.Require().NoError(err)
		s.Require().Len(spiffeIDList.Items, 1)

		// Verify the SPIFFE ID has changed
		expectedSpiffeID = makeID(s.trustDomain, "%s", test.second)
		actualSpiffeID = spiffeIDList.Items[0].Spec.SpiffeId
		s.Require().Equal(expectedSpiffeID, actualSpiffeID)

		// Delete Pod
		err = s.k8sClient.Delete(s.ctx, &pod)
		s.Require().NoError(err)
		s.reconcile(p)
	}
}

func (s *PodControllerTestSuite) reconcile(p *PodReconciler) error {
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      PodName,
			Namespace: PodNamespace,
		},
	}

	_, err := p.Reconcile(req)
	if err != nil {
		return err
	}
	s.Require().NoError(err)

	_, err = s.r.Reconcile(req)
	if err != nil {
		return err
	}
	s.Require().NoError(err)
	return nil
}
