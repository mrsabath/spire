//package main
package controllers

import (
	"fmt"
	"io/ioutil"
	"log"

	spiffeidv1beta1 "github.com/spiffe/spire/support/k8s/k8s-workload-registrar/mode-crd/api/spiffeid/v1beta1"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// attestor variants:
	nodeAttestor     = "nodeAttestor"
	workloadAttestor = "workloadAttestor"

	// selector labels
	namespaceLabel      = "Namespace"
	podUIDLabel         = "PodUID"
	podNameLabel        = "PodName"
	serviceAccountLabel = "ServiceAccount"
)

type IdentitySchema struct {
	Version string  `yaml:"version"`
	Fields  []Field `yaml:"fields"`
}

type Field struct {
	Name            string           `yaml:"name"`
	AttestorSource  *AttestorSource  `yaml:"attestorSource,omitempty"`
	ConfigMapSource *ConfigMapSource `yaml:"configMapSource,omitempty"`
}

type Source interface {
	GetValue(pod *corev1.Pod) (string, error)
}

type ConfigMapSource struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"ns"`
	Field     string `yaml:"field"`
}

type AttestorSource struct {
	Name    string    `yaml:"name"`
	Group   string    `yaml:"group"`
	Mapping []Mapping `yaml:"mapping"`
}

type Mapping struct {
	Type  string `yaml:"type"`
	Field string `yaml:"field"`
}

func (is *IdentitySchema) loadConfig(fileName string) (*IdentitySchema, error) {

	log.Print("identity_schema, loadConfig: before read")
	yamlFile, err := ioutil.ReadFile(fileName)
	if err != nil {
		log.Printf("identity_schema, loadConfig: Error reading yaml file %s:  %v ", fileName, err)
		return is, err
	}
	log.Print("identity_schema, loadConfig: after read")

	err = yaml.Unmarshal(yamlFile, is)
	if err != nil {
		//log.Fatalf("Unmarshal: %v", err)
		log.Printf("identity_schema, loadConfig: Error processing YAML file %v", err)
		return is, err
	}
	return is, nil
}

func main() {
	var is IdentitySchema

	// if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {

	if _, err := is.loadConfig("/tmp/identity-schema.yaml"); err != nil {
		log.Fatalf("Error getting IdenitySchema config %v", err)
	}

	// Set up pod:
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			ServiceAccountName: "podServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "podNamespace",
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
	}
	// if testCase.configLabel != "" && testCase.podLabel != "" {
	// 	pod.Labels[testCase.configLabel] = testCase.podLabel
	// }
	// if testCase.configAnnotation != "" && testCase.podAnnotation != "" {
	// 	pod.Annotations[testCase.configAnnotation] = testCase.podAnnotation
	// }

	// Test:
	//spiffeID := c.podSpiffeID(pod)

	finalId := is.getSVID(pod)
	log.Printf("** Final id %v", finalId)
	log.Printf("Identity %#v", is)
	fmt.Print(&is)
}

func (is *IdentitySchema) getSVID(pod *corev1.Pod) string {

	log.Printf("identity_schema, getId: processing Pod %s", pod.Name)

	var idString string = ""
	fields := is.Fields
	for i, field := range fields {
		log.Printf("identity_schema, getId: %d Field name: %s", i, field.Name)
		idString += "/" + is.getFieldValue(pod, field)
	}
	log.Printf("identity_schema, getSVID: ID Value: %s", idString)
	return idString
}

func (is *IdentitySchema) getFieldValue(pod *corev1.Pod, field Field) string {

	att := field.AttestorSource
	if att != nil {
		log.Printf("* identity_schema, getFieldValue: Field Attestor Group Name: %v", att.Group)
		value, err := att.GetValue(pod)
		if err != nil {
			log.Printf("* identity_schema, getFieldValue: Error processing the field %s, with attestor source %v", field.Name, err)
			// TODO for now, when error return the field name
			return field.Name
		}
		return value
	}

	cm := field.ConfigMapSource
	if cm != nil {
		log.Printf("* identity_schema, getFieldValue: ConfigMap Name %s", cm.Name)
		log.Printf("* identity_schema, getFieldValue: ConfigMap Field %s", cm.Field)
		log.Printf("* identity_schema, getFieldValue: ConfigMap Namespace %s", cm.Namespace)
		value, err := cm.GetValue(pod)
		if err != nil {
			log.Printf("* identity_schema, getFieldValue: Error processing the field %s, with configMap source: %v", field.Name, err)
			// TODO for now, when error return the field name
			return field.Name
		}
		return value
	}

	// TODO for now if value unknown, just return the field name
	log.Printf("* identity_schema, getFieldValue: Error processing the field %s, no matching source!", field.Name)
	return field.Name
}

func (att *AttestorSource) GetValue(pod *corev1.Pod) (value string, err error) {

	log.Printf("** identity_schema, GetValue(Attestor): pod: %s", pod.Name)
	// log.Printf("** This attestor uses mapping: %#v", attestor.Mapping)

	switch att.Group {
	case nodeAttestor:
		log.Print("**identity_schema, GetValue: Processing nodeAttestor")
		return "value-from-node-Attestor", nil
	case workloadAttestor:
		// here we don't need the selector values. We just need the value for the SPIFFE ID
		_, value, err = att.getValueFromWorkloadAttestor(pod)
		if err != nil {
			log.Printf("%s", err)
			return value, err
		} else {
			return value, nil
		}
	default:
		log.Print("** identity_schema, GetValue: Unknown attestor name")
		err := fmt.Errorf("Unknown attestor name: %s", att.Group)
		return value, err
	}
}

func (cms *ConfigMapSource) GetValue(pod *corev1.Pod) (value string, err error) {

	log.Printf("** identity_schema, GetValue(ConfigMap): ConfigMap namespace: %s, name: %s, field: %s", cms.Namespace, cms.Name, cms.Field)
	err = fmt.Errorf("Function not implemented for %s", "configMapSource")
	return value, err
}

// getValueFromWorkloadAttestor is a function used by both getSVID and getSelector
// in case of the getSVID, the selectorName should be ignored
// in case of the getSelector, values with empty selectorNames should be ignored
func (att *AttestorSource) getValueFromWorkloadAttestor(pod *corev1.Pod) (selectorName string, fieldValue string, err error) {

	log.Printf("*** identity_schema, getValueWorkloadAttestor for pod %s", pod.Name)
	for _, field := range att.Mapping {

		// only certain fields are valid as selectors, other will be ignored
		switch field.Type {
		case "k8s":
			switch field.Field {
			case "sa":
				return serviceAccountLabel, pod.Spec.ServiceAccountName, nil
			case "ns":
				return namespaceLabel, pod.Namespace, nil
			case "pod-name":
				return podNameLabel, pod.Name, nil
			case "pod-uid":
				return podUIDLabel, string(pod.UID), nil
			default:
				err := fmt.Errorf("Unknown field for k8s attestor: %s", field.Field)
				log.Printf("%s", err)
			}
		// to be used by other attestor types
		case "xxx":
			log.Printf("*** Processing xxx attestor")
		default:
			err := fmt.Errorf("Unknown attestor type: %s", field.Type)
			log.Printf("%s", err)
		}
	}
	return selectorName, fieldValue, fmt.Errorf("Cannot find a mapping match. Error: %s", err)
}

func (is *IdentitySchema) getSelector(pod *corev1.Pod) spiffeidv1beta1.Selector {

	log.Printf("*** identity_schema, getSelector :Creating Selectors for pod: %s", pod.Name)

	// create a new Selector object
	// always assign the NodeName value
	newSelector := spiffeidv1beta1.Selector{
		//PodUid:    pod.GetUID(),
		//Namespace: pod.Namespace,
		NodeName: pod.Spec.NodeName,
	}

	// iterrate through all the available fields and find the ones that are selector relevant:
	for _, field := range is.Fields {

		att := field.AttestorSource
		// Selector are only relevant for workloadAttestor
		if att != nil && att.Group == workloadAttestor {
			selectorName, selectorValue, err := att.getValueFromWorkloadAttestor(pod)
			if err != nil {
				log.Printf("*** identity_schema, getSelector Error getting selector: %v", err)
				continue
			}
			// some values are not relevant to selectors, so they will be skipped
			if selectorName == "" {
				log.Printf("*** identity_schema, getSelector: selectorName is empty, skipping selector for value=%s", selectorValue)
				continue
			}

			switch selectorName {
			case namespaceLabel:
				newSelector.Namespace = selectorValue
			case podUIDLabel:
				newSelector.PodUid = types.UID(selectorValue)
			case podNameLabel:
				newSelector.PodName = selectorValue
			case serviceAccountLabel:
				newSelector.ServiceAccount = selectorValue
			default:
				log.Printf("*** identity_schema, getSelector unknown selector %s = %s", selectorName, selectorValue)
			}
		}
	}
	return newSelector
}

// func getSelectorField(pod *corev1.Pod, attestor *AttestorSource) (selectorName string, selectorValue string, err error) {
// 	log.Printf("*** identity_schema, getSelectorField pod %s", pod.Name)

// 	if attestor.Mapping == nil {
// 		err := fmt.Errorf("Missing mapping for attestor: %s", attestor.Name)
// 		return selectorName, selectorValue, err
// 	}

// 	// iterrate thorugh all the mapping options and find appropriate selectors
// 	for _, field := range attestor.Mapping {

// 		//log.Printf("*** %d processing field: %#v", i, field)

// 		switch field.Type {
// 		case "k8s":
// 			switch field.Field {
// 			case "sa":
// 				return serviceAccountLabel, pod.Spec.ServiceAccountName, nil
// 			case "ns":
// 				return namespaceLabel, pod.Namespace, nil
// 			case "pod-name":
// 				return podNameLabel, pod.Name, nil
// 			case "pod-uid":
// 				return podUIDLabel, string(pod.UID), nil
// 			default:
// 				err := fmt.Errorf("Unknown field for k8s attestor: %s", field.Field)
// 				log.Printf("%s", err)
// 			}
// 		case "xxx":
// 			log.Printf("*** Processing xxx attestor")
// 		default:
// 			err := fmt.Errorf("Unknown attestor type: %s", field.Type)
// 			log.Printf("%s", err)
// 		}

// 	}
// 	log.Printf("No selectors found for attestor: %s", attestor.Name)
// 	return selectorName, selectorValue, nil
// }
