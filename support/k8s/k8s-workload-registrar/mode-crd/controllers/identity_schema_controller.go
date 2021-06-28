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

type IdentitySchemaController struct {
	Client client.Client
	Ctx    context.Context
	Log    logrus.FieldLogger
	Config IdentitySchemaConfig
}

type IdentitySchemaConfig struct {
	Version string  `yaml:"version"`
	Fields  []Field `yaml:"fields"`
}

type Field struct {
	Name             string                  `yaml:"name"`
	Value            string                  `yaml:"value,omitempty"`
	WorkloadAttestor *WorkloadAttestorSource `yaml:"workloadAttestorSource,omitempty"`
	NodeAttestor     *WorkloadAttestorSource `yaml:"nodeAttestorSource,omitempty"`
	ConfigMap        *ConfigMapSource        `yaml:"configMapSource,omitempty"`
}

type Source interface {
	GetValue(pod *corev1.Pod) (string, error)
}

type ConfigMapSource struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"ns"`
	Field     string `yaml:"field"`
}

type NodeAttestorSource struct {
	Name     string    `yaml:"name"`
	Mappings []Mapping `yaml:"mapping"`
}

type WorkloadAttestorSource struct {
	Name     string    `yaml:"name"`
	Mappings []Mapping `yaml:"mapping"`
}

type Mapping struct {
	Type  string `yaml:"type"`
	Field string `yaml:"field"`
}

func NewIdentitySchemaController(client client.Client, ctx context.Context, log logrus.FieldLogger, config IdentitySchemaConfig) *IdentitySchemaController {

	return &IdentitySchemaController{
		Client: client,
		Ctx:    ctx,
		Log:    log,
		Config: config,
	}
}

func loadConfig(fileName string) (*IdentitySchemaConfig, error) {

	is := IdentitySchemaConfig{}
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

func (is *IdentitySchemaController) getIdentityFormat(pod *corev1.Pod) (spiffeidv1beta1.Selector, string) {

	// create default selector if no identity schema fields available
	if is.Config.Fields == nil {
		newSelector := spiffeidv1beta1.Selector{}
		return newSelector, ""
	}

	// create a new Selector object
	// always assign the NodeName value
	newSelector := spiffeidv1beta1.Selector{
		//PodUid:    pod.GetUID(),
		//Namespace: pod.Namespace,
		NodeName: pod.Spec.NodeName,
	}

	is.Log.WithFields(logrus.Fields{
		"podName": pod.Name,
	}).Debug("Executing getSVID")

	var idString string = ""
	fields := is.Config.Fields
	for _, field := range fields {
		var val string = ""
		var name string = ""
		var err error
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
			name, val, err = is.getFieldInfo(pod, field)
			if err != nil {
				is.Log.WithFields(logrus.Fields{
					"podName": pod.Name,
				}).Errorf("Error retrieving value for the Field name=%s. %v", field.Name, err)

				// TODO for now, let's use the field name instead of the value, if not implemented or error
				val = field.Name
				is.Log.WithFields(logrus.Fields{
					"podName": pod.Name,
				}).Infof("Temporarly assigning Field name=%v as a value for this field.", field.Name)
			}

			// process selectors:
			switch name {
			case namespaceLabel:
				newSelector.Namespace = val
			case podUIDLabel:
				newSelector.PodUid = types.UID(val)
			case podNameLabel:
				newSelector.PodName = val
			case serviceAccountLabel:
				newSelector.ServiceAccount = val
			case "":
				is.Log.WithFields(logrus.Fields{
					"podName": pod.Name,
				}).Infof("Empty selector for Field name=%s. Skipping it", field.Name)

			default:
				is.Log.WithFields(logrus.Fields{
					"podName": pod.Name,
				}).Errorf("Unknown selector for the Field name=%s. Selector name=%s, value=%s", field.Name, name, val)
			}
		}
		idString += "/" + val
	}
	is.Log.WithFields(logrus.Fields{
		"podName":  pod.Name,
		"function": "getSVID",
	}).Debugf("SPIFFE ID=%s", idString)
	log.Printf("")
	return newSelector, idString
}

// getFieldInfo returns field name (used for selectors, might be empty if not applicable) and field value for SVID
func (is *IdentitySchemaController) getFieldInfo(pod *corev1.Pod, field Field) (name string, value string, err error) {

	// apply different functions based on field type:
	switch {
	case field.WorkloadAttestor != nil:
		// we can ignore the fieldLabel, since it's used by selectors only
		name, value, err := field.WorkloadAttestor.getValueFromWorkloadAttestor(is.Log, pod)
		if err != nil {
			is.Log.WithFields(logrus.Fields{
				"podName": pod.Name,
			}).Errorf("Error processing the attestor source field=%s: %v", field.Name, err)
			return "", field.Name, err
		}
		return name, value, nil
	case field.ConfigMap != nil:
		value, err := is.GetValueFromConfigMap(field.ConfigMap)
		if err != nil {
			is.Log.WithFields(logrus.Fields{
				"podName": pod.Name,
			}).Errorf("Error processing the configmMap source field=%s: %v", field.Name, err)
			return "", field.Name, err
		}
		return "", value, nil
	case field.NodeAttestor != nil:
		err := fmt.Errorf("NodeAttestor for a field %s is not implemented yet", field.Name)
		return "", field.Name, err
	default:
		err := fmt.Errorf("Unknown or missing source for field: %s", field.Name)
		return "", field.Name, err
	}
}

func (is *IdentitySchemaController) GetValueFromConfigMap(configMap *ConfigMapSource) (value string, err error) {

	// scope down the ConfigmMap list to the namespace provided in the configuration
	cmlist := corev1.ConfigMapList{}
	lopt := client.ListOptions{
		Namespace: configMap.Namespace,
	}
	if err := is.Client.List(is.Ctx, &cmlist, &lopt); err != nil {
		if !errors.IsNotFound(err) {
			log.Printf("Error, unable to get ConfigMap list")
		}
		// TODO: Address the permission error:
		// E0618 19:27:47.507321      14 reflector.go:178] pkg/mod/k8s.io/client-go@v0.18.2/tools/cache/reflector.go:125: Failed to list *v1.ConfigMap: configmaps is forbidden: User "system:serviceaccount:spire:spire-k8s-registrar" cannot list resource "configmaps" in API group "" at the cluster scope

	}
	// get all the configmaps in provided namespace
	for _, item := range cmlist.Items {

		if item.Name == configMap.Name {
			if item.Data == nil {
				log.Printf("Error, missing data field in configMap with name=%s", item.Name)
				continue
			}
			val := item.Data[configMap.Field]
			if val != "" {
				return val, nil
			}
			log.Printf("Data field %s not found in the ConfigMap %s", configMap.Field, configMap.Name)
		}

	}
	err = fmt.Errorf("No configMap with Name=%s in namespace %s found ", configMap.Name, configMap.Namespace)
	log.Printf("Error: %#v", err)
	return value, err
}

// getValueFromWorkloadAttestor is a function used by both getSVID and getSelector
// in case of the getSVID, the selectorName should be ignored
// in case of the getSelector, values with empty selectorNames should be ignored
func (watt *WorkloadAttestorSource) getValueFromWorkloadAttestor(log logrus.FieldLogger, pod *corev1.Pod) (fieldLabel string, fieldValue string, err error) {

	for _, field := range watt.Mappings {

		// only certain fields are valid as selectors, other will be ignored
		switch field.Type {
		case "k8s":
			// here we don't need the fieldLabel, since it's used only by selectors
			fieldLabel, fieldValue, err = watt.getValueFromK8s(field.Field, pod)
			if err != nil {
				log.Printf("Error retrieving k8s values for field=%s: %v", field.Field, err)
			}
			return fieldLabel, fieldValue, err
		// to be used by other attestor types
		case "xxx":
			// TODO to be removed...
			log.Printf("*** Processing xxx attestor")
		default:
			err := fmt.Errorf("Unknown attestor type: %s", field.Type)
			log.Printf("%s", err)
		}
	}
	return fieldLabel, fieldValue, fmt.Errorf("Cannot find a mapping match. Error: %s", err)
}

// getValueFromK8s - helper function to process values from K8s:
func (watt *WorkloadAttestorSource) getValueFromK8s(fieldName string, pod *corev1.Pod) (selectorName string, fieldValue string, err error) {
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
