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
	"log"
	"testing"

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
//PodName      string = "test-pod"
//PodNamespace string = "default"
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

// TestPodLabel adds a label to a pod and check if the SPIFFE ID is generated correctly.
// It then updates the label and ensures the SPIFFE ID is updated.
func (s *IdentitySchemaTestSuite) TestIdentitySchema() {
	tests := []struct {
		PodName       string
		PodNamespace  string
		PodLabel      string
		PodAnnotation string
		first         string
		second        string
		expectedSvid  string
		uid           string
	}{
		{
			PodName:      "test-label",
			PodNamespace: "default",
			PodLabel:     "spiffe",
			first:        "test-label",
			second:       "new-test-label",
			expectedSvid: "test-label",
			uid:          "123",
		},
		{
			PodName:       "test-annotation",
			PodNamespace:  "default",
			PodAnnotation: "spiffe",
			first:         "test-annotation",
			second:        "new-test-annotation",
			expectedSvid:  "test-annotation",
			uid:           "456",
		},
		{
			PodName:      "test-id-schema",
			PodNamespace: "default",
			expectedSvid: "minikube/eu-de/default/default/test-id-schema",
			uid:          "789",
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
	if err != nil {
		log.Printf("test, loadConfig: Error processing YAML file %v", err)

	}
	s.Require().NoError(err)

	// for _, test := range tests {
	for i, test := range tests {

		log.Printf("### executing test %v %s", i, test.expectedSvid)
		p := NewPodReconciler(PodReconcilerConfig{
			Client:        s.k8sClient,
			Cluster:       s.cluster,
			Ctx:           s.ctx,
			Log:           s.log,
			PodLabel:      test.PodLabel,
			PodAnnotation: test.PodAnnotation,
			Scheme:        s.scheme,
			TrustDomain:   s.trustDomain,
		})

		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        test.PodName,
				Namespace:   test.PodNamespace,
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
				ServiceAccountName: "default",
			},
		}
		err := s.k8sClient.Create(s.ctx, &pod)
		s.Require().NoError(err)
		s.reconcile(p, test.PodName, test.PodNamespace)

		// cInfo := corev1.ConfigMap{}
		// clusterFile := "../config/cluster-info.yaml"
		// log.Printf("test, before read, fileName: %s", clusterFile)
		// yamlFileCluster, err := ioutil.ReadFile(clusterFile)
		// if err != nil {
		// 	log.Printf("test, load File: Error reading yaml file %s:  %v ", clusterFile, err)
		// }
		// log.Printf("test cluster info: after read %s", yamlFileCluster)

		// err = yaml.Unmarshal(yamlFileCluster, &cInfo)
		// if err != nil {
		// 	log.Fatalf("Unmarshal: %v", err)
		// 	log.Printf("test, loadConfig: Error processing YAML file %v", err)
		// }

		// ObjectMeta: metav1.ObjectMeta{
		// 	Namespace:   "podNamespace",
		// 	Labels:      map[string]string{},
		// 	Annotations: map[string]string{},

		actualSvId := p.podSpiffeID(s.ctx, &pod)
		log.Printf("*** actualSvId %s ", actualSvId)
		// expectedSpiffeID := makeID(s.trustDomain, "%s", test.first)
		expectedSpiffeID := makeID(s.trustDomain, test.expectedSvid)
		s.Require().Equal(expectedSpiffeID, actualSvId)

		//actualSvId := is.getSVID(s.ctx, &pod, s.k8sClient)
		// expectedSvId := makeID(s.trustDomain, "%s/%s/%s/%s/%s", test.provider, test.region, test.namespace, test.sa, test.podName)
		// s.Require().Equal(expectedSvId, actualSvId)
		// s.reconcile(p)

		labelSelector := labels.Set(map[string]string{
			"podUid": string(pod.ObjectMeta.UID),
		})
		// Verify that exactly 1 SPIFFE ID  resource was created for this pod
		spiffeIDList := spiffeidv1beta1.SpiffeIDList{}
		err = s.k8sClient.List(s.ctx, &spiffeIDList, &client.ListOptions{
			LabelSelector: labelSelector.AsSelector(),
		})
		s.Require().NoError(err)
		log.Printf("**** Selectors %#v", spiffeIDList.Items[0].Spec.Selector)
		s.Require().Len(spiffeIDList.Items, 1)

		log.Print("**** ALL GOOD")

		// // TODO same results as above, verify if always use the same path
		// res, err := p.updateorCreatePodEntry(s.ctx, &pod)
		// if err != nil {
		// 	log.Printf("Error!!! %v", err)
		// }
		// log.Printf("**** updateCreate %#v", res)
		// s.reconcile(p, test.PodName, test.PodNamespace)
		// spiffeIDList = spiffeidv1beta1.SpiffeIDList{}
		// err = s.k8sClient.List(s.ctx, &spiffeIDList, &client.ListOptions{
		// 	LabelSelector: labelSelector.AsSelector(),
		// })
		// log.Printf("**** UPDATED Selectors %#v", spiffeIDList.Items[0].Spec.Selector)

		// // Verify the label/annotation matches what we expect
		// expectedSpiffeID := makeID(s.trustDomain, "%s", test.first)
		// actualSpiffeID := spiffeIDList.Items[0].Spec.SpiffeId
		// log.Printf("*** expectedSpiffeID %s ", expectedSpiffeID)
		// s.Require().Equal(expectedSpiffeID, actualSpiffeID)

		// // Update the labels/annotations
		// pod.Labels["spiffe"] = test.second
		// pod.Annotations["spiffe"] = test.second
		// err = s.k8sClient.Update(s.ctx, &pod)
		// s.Require().NoError(err)
		// s.reconcile(p)

		// // Verify that there is still exactly 1 SPIFFE ID resource for this pod
		// spiffeIDList = spiffeidv1beta1.SpiffeIDList{}
		// err = s.k8sClient.List(s.ctx, &spiffeIDList, &client.ListOptions{
		// 	LabelSelector: labelSelector.AsSelector(),
		// })
		// s.Require().NoError(err)
		// s.Require().Len(spiffeIDList.Items, 1)

		// // Verify the SPIFFE ID has changed
		// expectedSpiffeID = makeID(s.trustDomain, "%s", test.second)
		// actualSpiffeID = spiffeIDList.Items[0].Spec.SpiffeId
		// s.Require().Equal(expectedSpiffeID, actualSpiffeID)

		// Cleanup
		// Delete Pod
		err = s.k8sClient.Delete(s.ctx, &pod)
		s.Require().NoError(err)
		s.reconcile(p, test.PodName, PodNamespace)

	} // for

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
