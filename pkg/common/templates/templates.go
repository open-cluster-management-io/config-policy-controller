// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"github.com/golang/glog"
	"github.com/spf13/cast"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"regexp"
	"sigs.k8s.io/yaml"
	"strconv"
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
		"indent":           indent,
		"atoi":             atoi,
		"toInt":            toInt,
		"toBool":           toBool,
	}

	// create template processor and Initialize function map
	tmpl := template.New("tmpl").Funcs(funcMap)

	//convert the interface to yaml to string
	// ext.raw is jsonMarshalled data which the template processor is not accepting
	// so marshalling  unmarshalled(ext.raw) to yaml to string

	templateStr, err := toYAML(tmplMap)
	if err != nil {
		return "", err
	}
	glog.V(2).Infof("Initial template str to resolve : %v ", templateStr)

	//process for int or bool
	if strings.Contains(templateStr, "toInt") || strings.Contains(templateStr, "toBool") {
		templateStr = processForDataTypes(templateStr)
	}

	tmpl, err = tmpl.Parse(templateStr)
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
	glog.V(2).Infof("resolved template str : %v ", resolvedTemplateStr)
	//unmarshall before returning

	resolvedTemplateIntf, err := fromYAML(resolvedTemplateStr)
	if err != nil {
		return "", err
	}

	return resolvedTemplateIntf, nil
}

// fromYAML converts a YAML document into a map[string]interface{}.
func fromYAML(str string) (map[string]interface{}, error) {
	m := map[string]interface{}{}

	if err := yaml.Unmarshal([]byte(str), &m); err != nil {
		glog.Errorf("error parsing the YAML  the template str %v , \n %v ", str, err)
		return m, err
	}
	return m, nil
}

// ftoYAML converts a  map[string]interface{} to  YAML document string
func toYAML(v interface{}) (string, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		glog.Errorf("error parsing the YAML the template map %v , \n %v ", v, err)
		return "", err
	}

	return strings.TrimSuffix(string(data), "\n"), nil
}

func processForDataTypes(str string) string {

	//the idea is to remove the quotes enclosing the template if it ends in toBool ot ToInt
	//quotes around the resolved template forces the value to be a string..
	//so removal of these quotes allows yaml to process the datatype correctly..

	// the below pattern searches for optional block scalars | or >.. followed by the quoted template ,
	// and replaces it with just the template txt thats inside in the quotes
	// ex-1 key : "{{ "6" | toInt }}"  .. is replaced with  key : {{ "6" | toInt }}
	// ex-2 key : |
	//						"{{ "true" | toBool }}" .. is replaced with key : {{ "true" | toBool }}
	re := regexp.MustCompile(`:\s+(?:[\|>][-]?\s+)?(?:['|"]\s*)?({{.*?\s+\|\s+(?:toInt|toBool)\s*}})(?:\s*['|"])?`)
	glog.V(2).Infof("\n Pattern: %v\n", re.String())

	submatchall := re.FindAllStringSubmatch(str, -1)
	glog.V(2).Infof("\n All Submatches:\n%v", submatchall)

	processeddata := re.ReplaceAllString(str, ": $1")
	glog.V(2).Infof("\n processed data :\n%v", processeddata)

	return processeddata
}

func indent(spaces int, v string) string {
	pad := strings.Repeat(" ", spaces)
	npad := "\n" + pad + strings.Replace(v, "\n", "\n"+pad, -1)
	return strings.TrimSpace(npad)
}

func toInt(v interface{}) int {
	return cast.ToInt(v)
}

func atoi(a string) int {
	i, _ := strconv.Atoi(a)
	return i
}

func toBool(a string) bool {
	b, _ := strconv.ParseBool(a)
	return b
}
