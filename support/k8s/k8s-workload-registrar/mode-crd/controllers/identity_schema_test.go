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
	"testing"

	"github.com/spiffe/spire/support/k8s/k8s-workload-registrar/mode-crd/api/spiffeid/v1beta1"
	spiffeidv1beta1 "github.com/spiffe/spire/support/k8s/k8s-workload-registrar/mode-crd/api/spiffeid/v1beta1"
	"github.com/stretchr/testify/suite"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

const (
	isConfigFileTest = "../config/identity-schema.yaml"
)

func TestIdentitySchema(t *testing.T) {
	suite.Run(t, new(IdentitySchemaTestSuite))
}

type IdentitySchemaTestSuite struct {
	suite.Suite
	CommonControllerTestSuite
}

func (s *IdentitySchemaTestSuite) SetupSuite() {
	s.CommonControllerTestSuite = NewCommonControllerTestSuite(s.T())
}

// TestIdentitySchema create sample cases and checks if the SPIFFE ID is generated correctly.
func (s *IdentitySchemaTestSuite) TestIdentitySchema() {
	tests := []struct {
		podName       string
		podNamespace  string
		podLabel      string
		podAnnotation string
		sa            string
		first         string
		// second           string
		expectedSvid     string
		uid              string
		expectedSelector v1beta1.Selector
	}{
		{
			// simple, default identity, without the schema
			// using pod label
			podName:      "test-label",
			podNamespace: "default",
			podLabel:     "spiffe",
			sa:           "default",
			uid:          "123",
			first:        "test-label",
			//second:       "new-test-label",

			expectedSvid: "test-label",
			expectedSelector: v1beta1.Selector{
				PodUid:    "123",
				Namespace: "default",
				NodeName:  "test-node",
			},
		},
		{
			// simple, default identity, without the schema
			// using pod annotation
			podName:       "test-annotation",
			podNamespace:  "default",
			podAnnotation: "spiffe",
			first:         "test-annotation",
			//second:        "new-test-annotation",
			expectedSvid: "test-annotation",
			uid:          "456",
			expectedSelector: v1beta1.Selector{
				PodUid:    "456",
				Namespace: "default",
				NodeName:  NodeName,
			},
		},
		{
			// using default identity schema with default namespace and default serviceAccount
			podName:      "test-id-schema",
			podNamespace: "default",
			sa:           "default",
			expectedSvid: "minikube/eu-de/default/default/test-id-schema",
			uid:          "789",
			expectedSelector: v1beta1.Selector{
				PodName:        "test-id-schema",
				Namespace:      "default",
				ServiceAccount: "default",
				NodeName:       NodeName,
			},
		},
		{
			// using default identity schema with custom namespace and custom serviceAccount
			podName:      "test-id-schema",
			podNamespace: "testns",
			sa:           "testsa",
			expectedSvid: "minikube/eu-de/testns/testsa/test-id-schema",
			uid:          "012",
			expectedSelector: v1beta1.Selector{
				PodName:        "test-id-schema",
				Namespace:      "testns",
				ServiceAccount: "testsa",
				NodeName:       NodeName,
			},
		},
	}

	// create configMap with cluster info
	configMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-info",
			Namespace: "kube-system",
		},
		Data: map[string]string{"cluster-region": "eu-de"},
	}
	err := s.k8sClient.Create(s.ctx, &configMap)
	s.Require().NoError(err)

	for _, test := range tests {

		p := NewPodReconciler(PodReconcilerConfig{
			Client:                   s.k8sClient,
			Cluster:                  s.cluster,
			Ctx:                      s.ctx,
			Log:                      s.log,
			PodLabel:                 test.podLabel,
			PodAnnotation:            test.podAnnotation,
			Scheme:                   s.scheme,
			TrustDomain:              s.trustDomain,
			IdentitySchemaConfigFile: isConfigFileTest,
		})

		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        test.podName,
				Namespace:   test.podNamespace,
				Labels:      map[string]string{"spiffe": test.first},
				Annotations: map[string]string{"spiffe": test.first},
				UID:         types.UID(test.uid),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "test-pod",
					Image: "test-pod",
				}},
				NodeName:           NodeName,
				ServiceAccountName: test.sa,
			},
		}
		err := s.k8sClient.Create(s.ctx, &pod)
		s.Require().NoError(err)
		s.reconcile(p, test.podName, test.podNamespace)

		// check format created by Identity Schema:
		actualSvid := p.podSpiffeID(s.ctx, &pod)
		expectedSvid := makeID(s.trustDomain, test.expectedSvid)
		s.Require().Equal(expectedSvid, actualSvid)

		// create a label selector to correlate pods with CRs:
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

		// since we expect only one SpiffeId object on the list, it's safe to use the first one:
		actualSelector := spiffeIDList.Items[0].Spec.Selector
		s.Require().Equal(test.expectedSelector, actualSelector)

		// validate SPIFFE ID format in CR:
		actualSvid = spiffeIDList.Items[0].Spec.SpiffeId
		expectedSvid = makeID(s.trustDomain, test.expectedSvid)
		s.Require().Equal(expectedSvid, actualSvid)

		// Cleanup
		// Delete Pod
		err = s.k8sClient.Delete(s.ctx, &pod)
		s.Require().NoError(err)
		s.reconcile(p, test.podName, PodNamespace)
	} // end of for

	// delete the configmap
	err = s.k8sClient.Delete(s.ctx, &configMap)
	s.Require().NoError(err)
}

func (s *IdentitySchemaTestSuite) reconcile(p *PodReconciler, podName string, podNs string) {
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      podName,
			Namespace: podNs,
		},
	}

	_, err := p.Reconcile(req)
	s.Require().NoError(err)

	_, err = s.r.Reconcile(req)
	s.Require().NoError(err)
}
