//package main
package controllers

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/sirupsen/logrus"
	spiffeidv1beta1 "github.com/spiffe/spire/support/k8s/k8s-workload-registrar/mode-crd/api/spiffeid/v1beta1"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	isConfigFileDefault = "/run/identity-schema/config/identity-schema.yaml"

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
	Log     logrus.FieldLogger
	Version string  `yaml:"version"`
	Fields  []Field `yaml:"fields"`
}

type Field struct {
	Name            string           `yaml:"name"`
	Value           string           `yaml:"value,omitempty"`
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
	Name     string    `yaml:"name"`
	Group    string    `yaml:"group"`
	Mappings []Mapping `yaml:"mapping"`
}

type Mapping struct {
	Type  string `yaml:"type"`
	Field string `yaml:"field"`
}

func loadConfig(fileName string) (*IdentitySchema, error) {

	is := IdentitySchema{}
	yamlFile, err := ioutil.ReadFile(fileName)
	if err != nil {
		log.Printf("identity_schema, loadConfig: Error reading yaml file %s:  %v ", fileName, err)
		return &is, err
	}

	err = yaml.Unmarshal(yamlFile, &is)
	if err != nil {
		//log.Fatalf("Unmarshal: %v", err)
		log.Printf("identity_schema, loadConfig: Error processing YAML file %v", err)
		return &is, err
	}
	return &is, nil
}

func (is *IdentitySchema) getSVID(ctx context.Context, pod *corev1.Pod, cl client.Client) string {

	is.Log.WithFields(logrus.Fields{
		"podName": pod.Name,
	}).Debug("Executing getSVID")

	var idString string = ""
	fields := is.Fields
	for _, field := range fields {
		var val string = ""
		if field.Name == "" {
			is.Log.WithFields(logrus.Fields{
				"podName": pod.Name,
			}).Error("Identity schema Field with a missing Name. All the Fields must have names set. Ignoring this field.")
			continue
		}
		is.Log.WithFields(logrus.Fields{
			"podName": pod.Name,
		}).Debugf("Processing Field Name=%s", field.Name)

		if field.Value != "" {
			// field Value ovverides any value provided by other sources
			is.Log.WithFields(logrus.Fields{
				"podName": pod.Name,
			}).Infof("Field Name=%s has value provided: %s. Overriding all other sources.", field.Name, field.Value)
			val = field.Value
		} else {
			var err error
			val, err = is.getFieldValue(ctx, pod, cl, field)
			if err != nil {
				is.Log.WithFields(logrus.Fields{
					"podName": pod.Name,
				}).Errorf("Error retrieving value for the Field name=%s. %v", field.Name, err)

				// TODO for now, let's use the field name instead of the value
				val = field.Name
				is.Log.WithFields(logrus.Fields{
					"podName": pod.Name,
				}).Infof("Temporarly assigning Field name=%v as a value for this field.", field.Name)
			}
		}
		idString += "/" + val
	}
	is.Log.WithFields(logrus.Fields{
		"podName":  pod.Name,
		"function": "getSVID",
	}).Debugf("SPIFFE ID=%s", idString)
	return idString
}

func (is *IdentitySchema) getFieldValue(ctx context.Context, pod *corev1.Pod, cl client.Client, field Field) (string, error) {

	switch {
	case field.AttestorSource != nil:
		value, err := field.AttestorSource.GetValue(pod)
		if err != nil {
			is.Log.WithFields(logrus.Fields{
				"podName": pod.Name,
			}).Errorf("Error processing the attestor source field=%s: %v", field.Name, err)
			return field.Name, err
		}
		return value, nil
	case field.ConfigMapSource != nil:
		value, err := field.ConfigMapSource.GetValue(ctx, cl, field.ConfigMapSource)
		if err != nil {
			is.Log.WithFields(logrus.Fields{
				"podName": pod.Name,
			}).Errorf("Error processing the configmMap source field=%s: %v", field.Name, err)
			return field.Name, err
		}
		return value, nil
	default:
		err := fmt.Errorf("Unknown or missing source for field: %s", field.Name)
		return field.Name, err
	}
}

func (att *AttestorSource) GetValue(pod *corev1.Pod) (value string, err error) {

	switch att.Group {
	case nodeAttestor:
		err = fmt.Errorf("Function for %s not implemented", "nodeAttestor")
		return "value-from-node-Attestor", err
	case workloadAttestor:
		// here we don't need the selector values. We just need the value for the SPIFFE ID
		_, value, err = att.getValueFromWorkloadAttestor(pod)
		if err != nil {
			log.Printf("Error: %s", err)
			return value, err
		} else {
			return value, nil
		}
	default:
		err := fmt.Errorf("Unknown attestor name: %s", att.Group)
		log.Printf("Error getting attestor: %v", err)
		return value, err
	}
}

func (cms *ConfigMapSource) GetValue(ctx context.Context, cl client.Client, source *ConfigMapSource) (value string, err error) {

	// scope down the ConfigmMap list to the namespace provided in the configuration
	cmlist := corev1.ConfigMapList{}
	lopt := client.ListOptions{
		Namespace: source.Namespace,
	}
	if err := cl.List(ctx, &cmlist, &lopt); err != nil {
		if !errors.IsNotFound(err) {
			log.Printf("Error, unable to get ConfigMap list")
		}
		// TODO: Address the permission error:
		// E0618 19:27:47.507321      14 reflector.go:178] pkg/mod/k8s.io/client-go@v0.18.2/tools/cache/reflector.go:125: Failed to list *v1.ConfigMap: configmaps is forbidden: User "system:serviceaccount:spire:spire-k8s-registrar" cannot list resource "configmaps" in API group "" at the cluster scope

	}
	// get all the configmaps in provided namespace
	for _, item := range cmlist.Items {

		if item.Name == source.Name {
			if item.Data == nil {
				log.Printf("Error, missing data field in configMap with name=%s", item.Name)
				continue
			}
			val := item.Data[source.Field]
			if val != "" {
				return val, nil
			}
			log.Printf("Data field %s not found in the ConfigMap %s", source.Field, source.Name)
		}

	}
	err = fmt.Errorf("No configMap with Name=%s in namespace %s found ", source.Name, source.Namespace)
	log.Printf("Error: %#v", err)
	return value, err
}

// getValueFromWorkloadAttestor is a function used by both getSVID and getSelector
// in case of the getSVID, the selectorName should be ignored
// in case of the getSelector, values with empty selectorNames should be ignored
func (att *AttestorSource) getValueFromWorkloadAttestor(pod *corev1.Pod) (selectorName string, fieldValue string, err error) {

	for _, field := range att.Mappings {

		// only certain fields are valid as selectors, other will be ignored
		switch field.Type {
		case "k8s":
			selectorName, fieldValue, err = getValueFromK8s(field.Field, pod)
			if err != nil {
				log.Printf("Error retrieving k8s values for field=%s: %v", field.Field, err)
			}
			return selectorName, fieldValue, err
		// to be used by other attestor types
		case "xxx":
			// TODO to be removed...
			log.Printf("*** Processing xxx attestor")
		default:
			err := fmt.Errorf("Unknown attestor type: %s", field.Type)
			log.Printf("%s", err)
		}
	}
	return selectorName, fieldValue, fmt.Errorf("Cannot find a mapping match. Error: %s", err)
}

// getValueFromK8s - helper function to process values from K8s:
func getValueFromK8s(fieldName string, pod *corev1.Pod) (selectorName string, fieldValue string, err error) {
	switch fieldName {
	case "sa":
		return serviceAccountLabel, pod.Spec.ServiceAccountName, nil
	case "ns":
		return namespaceLabel, pod.Namespace, nil
	case "pod-name":
		return podNameLabel, pod.Name, nil
	case "pod-uid":
		return podUIDLabel, string(pod.UID), nil
	default:
		err := fmt.Errorf("Unknown field for k8s attestor: %s", fieldName)
		log.Printf("%s", err)
		return selectorName, fieldValue, err
	}
}

func (is *IdentitySchema) getSelector(pod *corev1.Pod) spiffeidv1beta1.Selector {

	// create default selector if no identity schema fields available
	if is.Fields == nil {
		newSelector := spiffeidv1beta1.Selector{
			PodUid:    pod.GetUID(),
			Namespace: pod.Namespace,
			NodeName:  pod.Spec.NodeName,
		}
		return newSelector
	}

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
		// Selectors are only relevant for workloadAttestor
		if att != nil && att.Group == workloadAttestor {
			selectorName, selectorValue, err := att.getValueFromWorkloadAttestor(pod)
			if err != nil {
				is.Log.WithFields(logrus.Fields{
					"podName": pod.Name,
				}).Errorf("Error retrieving selector value for the Field name=%s. %v", field.Name, err)
				continue
			}
			// some values are not relevant to selectors, so they will be skipped
			if selectorName == "" {
				is.Log.WithFields(logrus.Fields{
					"podName": pod.Name,
				}).Debugf("Selector name for the Field name=%s is empty. Skiping it.", field.Name)
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
				is.Log.WithFields(logrus.Fields{
					"podName": pod.Name,
				}).Errorf("Unknown selector for the Field name=%s. Selector name=%s, value=%s", field.Name, selectorName, selectorValue)
			}
		}
	}
	return newSelector
}
