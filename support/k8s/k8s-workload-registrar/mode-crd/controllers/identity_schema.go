//package main
package controllers

import (
	"fmt"
	"io/ioutil"
	"log"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	GetValue(pod *corev1.Pod, fieldName string) (string, error)
}

// type AttestorSource struct {
// 	Name     string    `yaml:"name"`
// 	Attestor *Attestor `yaml:"attestor,omitempty"`
// }

// type ConfigMapSource struct {
// 	Name      string     `yaml:"name"`
// 	ConfigMap *ConfigMap `yaml:"configMap,omitempty"`
// }

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

	log.Print("before read")
	yamlFile, err := ioutil.ReadFile(fileName)
	if err != nil {
		log.Printf("Error reading yaml file %s:  %v ", fileName, err)
		return is, err
	}
	log.Print("after read")

	err = yaml.Unmarshal(yamlFile, is)
	if err != nil {
		//log.Fatalf("Unmarshal: %v", err)
		log.Printf("Error processing YAML file %v", err)
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

	finalId := is.getId(pod)
	log.Printf("** Final id %v", finalId)
	log.Printf("Identity %#v", is)
	fmt.Print(&is)
}

func (is *IdentitySchema) getId(pod *corev1.Pod) string {

	// log.Printf("Processing Pod %#v", pod)

	var idString string = ""
	fields := is.Fields
	for i, field := range fields {
		log.Printf("%d Field name: %s", i, field.Name)
		// log.Printf("%d Field source: %v", i, field.)

		idString += "/" + is.getFieldValue(pod, field)
	}
	log.Printf("ID Value: %s", idString)
	return idString
}

func (is *IdentitySchema) getFieldValue(pod *corev1.Pod, field Field) string {
	att := field.AttestorSource
	if att != nil {
		log.Printf("* Field Attestor Group Name: %v", att.Group)
		return att.GetValue(pod, field.Name)
	}

	cm := field.ConfigMapSource
	if cm != nil {
		log.Printf("* ConfigMap Name %s", cm.Name)
		log.Printf("* ConfigMap Field %s", cm.Field)
		log.Printf("* ConfigMap Namespace %s", cm.Namespace)
		return cm.GetValue(pod, field.Name)
	}

	// TODO for now if value unknown, just return the field name
	return field.Name
}

//func (at *AttestorSource) getValue(pod *corev1.Pod, name string, attestor *Attestor) string {
func (at *AttestorSource) GetValue(pod *corev1.Pod, fieldName string) string {
	// log.Printf("** Attestor group: %s", attestor.Group)
	// log.Printf("** This attestor uses mapping: %#v", attestor.Mapping)

	switch at.Group {
	case "nodeAttestor":
		log.Print("** Processing nodeAttestor")
		return "value-from-node-Attestor"
	case "workloadAttestor":
		// if _, err := idSchema.loadConfig("/run/identity-schema/config/identity-schema.yaml"); err != nil {
		value, err := at.getValueFromWorkloadAttestor(pod, at.Mapping)
		if err != nil {
			log.Printf("%s", err)
		} else {
			return value
		}

	default:
		log.Print("** Unknown attestor name")
	}
	// TODO for now if value unknown, just return the field name
	return fieldName
}

func (cms *ConfigMapSource) GetValue(pod *corev1.Pod, fieldName string) string {
	log.Printf("** ConfigMap namespace: %s, name: %s, field: %s", cms.Namespace, cms.Name, cms.Field)

	// TODO for now, just return the field name
	return fieldName
}

func (at *AttestorSource) getValueFromWorkloadAttestor(pod *corev1.Pod, mapping []Mapping) (msg string, err error) {

	for _, field := range mapping {

		//log.Printf("*** %d processing field: %#v", i, field)

		switch field.Type {
		case "k8s":
			switch field.Field {
			case "sa":
				return pod.Spec.ServiceAccountName, nil
			case "ns":
				return pod.Namespace, nil
			case "pod-name":
				return pod.Name, nil
			case "pod-uid":
				return string(pod.UID), nil
			default:
				err := fmt.Errorf("Unknown field for k8s attestor: %s", field.Field)
				log.Printf("%s", err)
			}
		case "xxx":
			log.Printf("*** Processing xxx attestor")
		default:
			err := fmt.Errorf("Unknown attestor type: %s", field.Type)
			log.Printf("%s", err)
		}

	}
	return msg, fmt.Errorf("Cannot find a mapping match. Error: %s", err)
}
