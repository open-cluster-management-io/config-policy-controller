// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
	"strings"
	"text/template"
)

var kubeClient *kubernetes.Interface
var kubeConfig *rest.Config
var kubeAPIResourceList []*metav1.APIResourceList

func InitializeKubeClient(kClient *kubernetes.Interface, kConfig *rest.Config) {
	kubeClient = kClient
	kubeConfig = kConfig
}

//If this is set, template processing will not try to rediscover
// the apiresourcesList needed for dynamic client/ gvk look
func SetAPIResources(apiresList []*metav1.APIResourceList) {
	kubeAPIResourceList = apiresList
}

// just does a simple check for {{ string to indicate if it has a template
func HasTemplate(templateStr string) bool {
	glog.V(2).Infof("hasTemplate template str:  %v", templateStr)

	hasTemplate := false
	if strings.Contains(templateStr, "{{") {
		hasTemplate = true
	}

	glog.V(2).Infof("hasTemplate: %v", hasTemplate)
	return hasTemplate
}

// Main Template Processing func
func ResolveTemplate(tmplMap interface{}) (interface{}, error) {

	glog.V(2).Infof("ResolveTemplate for: %v", tmplMap)

	// Build Map of supported template functions
	funcMap := template.FuncMap{
		"fromSecret":       fromSecret,
		"fromConfigMap":    fromConfigMap,
		"fromClusterClaim": fromClusterClaim,
		"lookup":           lookup,
		"base64enc":        base64encode,
		"base64dec":        base64decode,
	}

	// create template processor and Initialize function map
	tmpl := template.New("tmpl").Funcs(funcMap)

	//convert the interface to yaml to string
	// ext.raw is jsonMarshalled data which the template processor is not accepting
	// so marshalling  unmarshalled(ext.raw) to yaml to string

	templateStr := toYAML(tmplMap)

	tmpl, err := tmpl.Parse(templateStr)
	if err != nil {
		glog.Errorf("error parsing template map %v,\n template str %v,\n error: %v", tmplMap, templateStr, err)
		return "", err
	}

	var buf strings.Builder
	err = tmpl.Execute(&buf, "")
	if err != nil {
		glog.Errorf("error executing the template map %v,\n template str %v,\n error: %v", tmplMap, templateStr, err)
		return "", err
	}

	resolvedTemplateStr := buf.String()
	glog.V(2).Infof("resolved template: %v ", resolvedTemplateStr)

	//unmarshall before returning
	return fromYAML(resolvedTemplateStr), nil
}

// fromYAML converts a YAML document into a map[string]interface{}.
func fromYAML(str string) map[string]interface{} {
	m := map[string]interface{}{}

	if err := yaml.Unmarshal([]byte(str), &m); err != nil {
		glog.Error(err)
	}
	return m
}

// ftoYAML converts a  map[string]interface{} to  YAML document string
func toYAML(v interface{}) string {
	data, err := yaml.Marshal(v)
	if err != nil {
		glog.Error(err)
	}

	return strings.TrimSuffix(string(data), "\n")
}
