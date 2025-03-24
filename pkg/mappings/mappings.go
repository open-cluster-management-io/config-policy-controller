// Copyright Contributors to the Open Cluster Management project

package mappings

import (
	_ "embed"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"
)

// APIMapping stores information required for mapping between GroupVersionKinds
// and GroupVersionResources, as well as whether the API is namespaced.
type APIMapping struct {
	Group    string   `json:"group"`
	Version  string   `json:"version"`
	Kind     string   `json:"kind"`
	Singular string   `json:"singular"`
	Plural   string   `json:"plural"`
	Scope    Scope    `json:"scope"`
	Verbs    []string `json:"verbs,omitempty"`
}

// DefaultVerbs is a very common set of verbs, used to simplify the mappings file.
var DefaultVerbs = []string{
	"create", "delete", "deletecollection", "get", "list", "patch", "update", "watch",
}

func (a APIMapping) String() string {
	return a.Group + "/" + a.Version + " " + a.Plural
}

// ResourceLists takes a list of APIMappings and uses them to populate
// APIResourceLists which can be used in a fake discovery client.
func ResourceLists(mappings []APIMapping) []*metav1.APIResourceList {
	resourceListMapping := map[schema.GroupVersion][]metav1.APIResource{}

	for _, mapping := range mappings {
		gv := schema.GroupVersion{
			Group:   mapping.Group,
			Version: mapping.Version,
		}

		verbs := mapping.Verbs

		// It seems safe to interpret an empty list this way, since a resource
		// with no verbs would otherwise be nonsensical.
		if len(verbs) == 0 {
			verbs = DefaultVerbs
		}

		resourceListMapping[gv] = append(resourceListMapping[gv], metav1.APIResource{
			Kind:         mapping.Kind,
			Name:         mapping.Plural,
			SingularName: mapping.Singular,
			Namespaced:   mapping.Scope == "namespace",
			Verbs:        verbs,
		})
	}

	resourceLists := []*metav1.APIResourceList{}

	for gv := range resourceListMapping {
		resourceLists = append(resourceLists, &metav1.APIResourceList{
			GroupVersion: gv.String(),
			APIResources: resourceListMapping[gv],
		})
	}

	return resourceLists
}

//go:embed default-mappings.yaml
var defaultMappings []byte

// DefaultResourceLists returns APIResourceLists which can be used in a fake
// discovery client.
func DefaultResourceLists() ([]*metav1.APIResourceList, error) {
	mappings := []APIMapping{}

	if err := yaml.Unmarshal(defaultMappings, &mappings); err != nil {
		return nil, err
	}

	resList := ResourceLists(mappings)

	return resList, nil
}

// GenerateMappings connects to a Kubernetes cluster and discovers the available
// api-resources, printing out that information as a YAML list of APIMappings.
// The cluster connected to follows the usual conventions, eg it can be set with
// the KUBECONFIG environment variable. This function is meant to be used inside
// of a cobra-style CLI.
func GenerateMappings(cmd *cobra.Command, _ []string) error {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})

	kubeConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return err
	}

	client, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	_, resources, _, err := client.DiscoveryClient.GroupsAndMaybeResources()
	if err != nil {
		return err
	}

	apiMappings := []APIMapping{}

	for gv, resList := range resources {
		for _, res := range resList.APIResources {
			if strings.Contains(res.Name, "/") {
				continue // skip subresources
			}

			scoping := meta.RESTScopeNameRoot
			if res.Namespaced {
				scoping = meta.RESTScopeNameNamespace
			}

			slices.Sort(res.Verbs)

			verbs := res.Verbs

			if slices.Equal(res.Verbs, DefaultVerbs) {
				verbs = []string{}
			}

			apiMappings = append(apiMappings, APIMapping{
				Group:    gv.Group,
				Version:  gv.Version,
				Kind:     res.Kind,
				Singular: res.SingularName,
				Plural:   res.Name,
				Scope:    Scope(scoping),
				Verbs:    verbs,
			})
		}
	}

	slices.SortFunc(apiMappings, func(a, b APIMapping) int {
		if a.String() < b.String() {
			return -1
		}

		return 1
	})

	out, err := yaml.Marshal(apiMappings)
	if err != nil {
		return err
	}

	cmd.Print(string(out))

	return nil
}

type Scope string

func (s Scope) Name() meta.RESTScopeName {
	return meta.RESTScopeName(s)
}
