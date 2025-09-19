// Copyright Contributors to the Open Cluster Management project

package dryrun

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/spf13/cobra"
	"github.com/stolostron/go-log-utils/zaputil"
	depclient "github.com/stolostron/kubernetes-dependency-watches/client"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	yaml "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	clientsetfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	klog "k8s.io/klog/v2"
	parentpolicyv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	runtime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	k8syaml "sigs.k8s.io/yaml"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	ctrl "open-cluster-management.io/config-policy-controller/controllers"
	"open-cluster-management.io/config-policy-controller/pkg/common"
	"open-cluster-management.io/config-policy-controller/pkg/mappings"
)

func (d *DryRunner) dryRun(cmd *cobra.Command, args []string) error {
	// The "usage" output will still be emitted if a required flag is missing,
	// or if an unknown flag was passed.
	cmd.SilenceUsage = true

	cfgPolicy, err := d.readPolicy(cmd)
	if err != nil {
		return fmt.Errorf("unable to read input policy: %w", err)
	}

	inputObjects, err := d.readInputResources(cmd, args)
	if err != nil {
		return fmt.Errorf("unable to read input resources: %w", err)
	}

	if err := d.setupLogs(); err != nil {
		return fmt.Errorf("unable to setup the logging configuration: %w", err)
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	rec, err := d.setupReconciler(ctx, cfgPolicy)
	if err != nil {
		return fmt.Errorf("unable to setup the dryrun reconciler: %w", err)
	}

	err = d.applyInputResources(ctx, rec, inputObjects)
	if err != nil {
		return fmt.Errorf("unable to apply input resources: %w", err)
	}

	cfgPolicyNN := types.NamespacedName{
		Name:      cfgPolicy.GetName(),
		Namespace: cfgPolicy.GetNamespace(),
	}

	if _, err := rec.Reconcile(ctx, runtime.Request{NamespacedName: cfgPolicyNN}); err != nil {
		return fmt.Errorf("unable to complete the dryrun reconcile: %w", err)
	}

	if err := rec.Get(ctx, cfgPolicyNN, cfgPolicy); err != nil {
		return fmt.Errorf("unable to get the resulting policy state: %w", err)
	}

	if d.desiredStatus != "" {
		if err := d.compareStatus(cmd, cfgPolicy.Status); err != nil {
			return fmt.Errorf("unable to compare desired status: %w", err)
		}

		cmd.Print("\n")
	}

	if d.statusPath != "" {
		if err := d.saveStatus(cfgPolicy.Status); err != nil {
			return fmt.Errorf("unable to save the resulting policy state: %w", err)
		}
	}

	if d.printDiffs {
		d.outputDiffs(cmd, cfgPolicy.Status)
	}

	if err := d.saveOrPrintComplianceMessages(ctx, cmd, rec.Client, cfgPolicy.Namespace); err != nil {
		return fmt.Errorf("unable to save or print the compliance messages: %w", err)
	}

	if cfgPolicy.Status.ComplianceState != policyv1.Compliant {
		return ErrNonCompliant
	}

	return nil
}

const parentName string = "cfgpol-dryrun-parent"

// readPolicy reads the policy file specified in the command flags, ensures that it is either a
// ConfigurationPolicy or a Policy with exactly one ConfigurationPolicy template, and returns that
// ConfigurationPolicy object after overriding the remediationAction to `inform`.
func (d *DryRunner) readPolicy(cmd *cobra.Command) (*policyv1.ConfigurationPolicy, error) {
	reader, err := os.Open(d.policyPath)
	if err != nil {
		return nil, err
	}

	policyBytes, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	unstruct := unstructured.Unstructured{}

	if err := k8syaml.UnmarshalStrict(policyBytes, &unstruct.Object); err != nil {
		return nil, err
	}

	if unstruct.GetAPIVersion() != "policy.open-cluster-management.io/v1" {
		return nil, fmt.Errorf("unsupported apiVersion: %v, the input policy must be "+
			"'policy.open-cluster-management.io/v1'", unstruct.GetAPIVersion())
	}

	cfgpol := policyv1.ConfigurationPolicy{}

	switch unstruct.GetKind() {
	case "ConfigurationPolicy":
		if err := k8syaml.UnmarshalStrict(policyBytes, &cfgpol); err != nil {
			return nil, fmt.Errorf("could not unmarshal input to a ConfigurationPolicy: %w", err)
		}
	case "Policy":
		tmpls, found, err := unstructured.NestedSlice(unstruct.Object, "spec", "policy-templates")
		if err != nil {
			return nil, fmt.Errorf("invalid input Policy: %w", err)
		}

		if !found {
			return nil, errors.New("invalid input Policy: no policy-templates found")
		}

		cfgPolFound := false

		for i, tmpl := range tmpls {
			tmplMap, ok := tmpl.(map[string]any)
			if !ok {
				continue
			}

			objDef, ok := tmplMap["objectDefinition"]
			if !ok {
				continue
			}

			objDefMap, ok := objDef.(map[string]any)
			if !ok {
				continue
			}

			if objDefMap["kind"] != "ConfigurationPolicy" {
				continue
			}

			if cfgPolFound {
				cmd.Println("Ignoring additional ConfigurationPolicy in input policy")

				continue
			}

			cfgPolFound = true

			cfgpolBytes, err := json.Marshal(objDef)
			if err != nil {
				return nil, errors.New("unable to marshal policy template back to JSON")
			}

			if err := k8syaml.UnmarshalStrict(cfgpolBytes, &cfgpol); err != nil {
				return nil, fmt.Errorf("could not unmarshal input policy template [%v] to a "+
					"ConfigurationPolicy: %w", i, err)
			}
		}

		if !cfgPolFound {
			return nil, errors.New("invalid input Policy: it must contain a ConfigurationPolicy")
		}
	default:
		return nil, fmt.Errorf("unsupported input kind: %v, must be 'Policy' or 'ConfigurationPolicy'",
			unstruct.GetKind())
	}

	cfgpol.Spec.RemediationAction = policyv1.Inform
	cfgpol.Spec.EvaluationInterval = policyv1.EvaluationInterval{
		Compliant:    "10s",
		NonCompliant: "10s",
	}

	cfgpol.OwnerReferences = []metav1.OwnerReference{{
		Name: parentName,
	}}

	return &cfgpol, nil
}

// readInputResources takes stdin and any paths given as "positional" arguments,
// and decodes them from YAML into k8s resources. Directories can be passed in
// arguments: in this case all files in that directory will be read and decoded
// (but no "deeper" directories are considered).
func (d *DryRunner) readInputResources(cmd *cobra.Command, args []string) (
	[]*unstructured.Unstructured, error,
) {
	stdinInfo, err := os.Stdin.Stat()
	if err != nil {
		return nil, fmt.Errorf("could not read stdin: %w", err)
	}

	rawInputs := []*unstructured.Unstructured{}

	filepaths := []string{}

	if stdinInfo.Mode()&os.ModeCharDevice == 0 {
		filepaths = append(filepaths, "-")
	}

	for _, arg := range args {
		if arg == "-" {
			filepaths = append(filepaths, "-")

			continue
		}

		info, err := os.Stat(arg)
		if err != nil {
			return nil, fmt.Errorf("could not open file %v: %w", arg, err)
		}

		if info.IsDir() {
			nestedFiles, err := os.ReadDir(arg)
			if err != nil {
				return nil, fmt.Errorf("could not open directory %v: %w", arg, err)
			}

			for _, f := range nestedFiles {
				if f.IsDir() {
					continue // ignore deeply-nested files
				}

				filepaths = append(filepaths, filepath.Join(arg, f.Name()))
			}
		} else {
			filepaths = append(filepaths, arg)
		}
	}

	for _, inpPath := range filepaths {
		var r io.Reader

		pathToPrint := "file " + inpPath

		if inpPath == "-" {
			r = cmd.InOrStdin()
			pathToPrint = "stdin"
		} else {
			r, err = os.Open(inpPath) // #nosec G304
			if err != nil {
				return nil, fmt.Errorf("could not open %v: %w", pathToPrint, err)
			}
		}

		// This is complicated because sigs.k8s.io/yaml does not provide a decoder, which we need
		// for extracting multiple objects from single files, but gopkg.in/yaml.v3 does not
		// specially handle certain types as expected by kubernetes converters (for example, a
		// converter might require `int64`, instead of just `int`). So this decodes twice, in order
		// to get both behaviors.

		d := yaml.NewDecoder(r)

		for {
			var obj map[string]any

			err := d.Decode(&obj)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}

				return nil, fmt.Errorf("could not decode %v to YAML: %w", pathToPrint, err)
			}

			objJSON, err := json.Marshal(obj)
			if err != nil {
				return nil, fmt.Errorf("could not re-marshal %v to JSON: %w", pathToPrint, err)
			}

			var k8sObj unstructured.Unstructured

			if err := k8syaml.UnmarshalStrict(objJSON, &k8sObj); err != nil {
				return nil, fmt.Errorf("could not re-un-marshal %v to kubernetes YAML: %w", pathToPrint, err)
			}

			if obj != nil {
				rawInputs = append(rawInputs, &k8sObj)
			}
		}
	}

	return rawInputs, nil
}

func (d *DryRunner) applyInputResources(
	ctx context.Context, rec *ctrl.ConfigurationPolicyReconciler, inputObjects []*unstructured.Unstructured,
) error {
	// Apply the user's resources to the fake cluster
	for _, obj := range inputObjects {
		gvk := obj.GroupVersionKind()

		scopedGVR, err := rec.DynamicWatcher.GVKToGVR(gvk)
		if err != nil {
			if errors.Is(err, depclient.ErrNoVersionedResource) {
				return fmt.Errorf("%w for kind %v: if this is a custom resource, it may need an "+
					"entry in the mappings file", err, gvk.Kind)
			}

			return fmt.Errorf("unable to apply an input resource: %w", err)
		}

		var resInt dynamic.ResourceInterface

		if scopedGVR.Namespaced {
			if obj.GetNamespace() == "" {
				obj.SetNamespace("default")
			}

			resInt = rec.TargetK8sDynamicClient.Resource(scopedGVR.GroupVersionResource).Namespace(obj.GetNamespace())
		} else {
			resInt = rec.TargetK8sDynamicClient.Resource(scopedGVR.GroupVersionResource)
		}

		sanitizeForCreation(obj)

		if _, err := resInt.Create(ctx, obj, metav1.CreateOptions{}); err != nil &&
			!k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("unable to apply an input resource: %w", err)
		}

		// Manually convert resources from the dynamic client to the runtime client
		err = rec.Client.Create(ctx, obj)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	return nil
}

// setupLogs configures klog and the controller-runtime logger to send logs to the
// path defined in the configuration. If that option is empty, logs will be discarded.
func (d *DryRunner) setupLogs() error {
	if d.logPath == "" {
		klog.SetLogger(logr.Discard())
		runtime.SetLogger(logr.Discard())

		return nil
	}

	z := zaputil.NewFlagConfig()
	cfg := z.GetConfig()

	cfg.Level = zap.NewAtomicLevelAt(zapcore.Level(-1))
	cfg.Encoding = "console"
	cfg.OutputPaths = []string{d.logPath}

	ctrlZap, err := cfg.Build()
	if err != nil {
		return err
	}

	logger := zapr.NewLogger(ctrlZap)

	runtime.SetLogger(logger)
	klog.SetLogger(logger)

	return nil
}

//go:embed policy.open-cluster-management.io_configurationpolicies.yaml
var configPolDefinitionYaml []byte

func (d *DryRunner) setupReconciler(
	ctx context.Context, cfgPolicy *policyv1.ConfigurationPolicy,
) (*ctrl.ConfigurationPolicyReconciler, error) {
	if err := policyv1.AddToScheme(scheme.Scheme); err != nil {
		return nil, err
	}

	if err := extv1.AddToScheme(scheme.Scheme); err != nil {
		return nil, err
	}

	configPolCRD := &extv1.CustomResourceDefinition{}

	if err := k8syaml.UnmarshalStrict(configPolDefinitionYaml, configPolCRD); err != nil {
		return nil, err
	}

	dynamicClient := dynfake.NewSimpleDynamicClient(scheme.Scheme)
	clientset := clientsetfake.NewSimpleClientset()
	watcherReconciler, _ := depclient.NewControllerRuntimeSource()

	dynamicWatcher := depclient.NewWithClients(
		dynamicClient,
		clientset.Discovery(),
		watcherReconciler,
		&depclient.Options{DisableInitialReconcile: true, EnableCache: true},
	)

	go func() {
		err := dynamicWatcher.Start(ctx)
		if err != nil {
			panic(err)
		}
	}()

	runtimeClient := clientfake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(configPolCRD, cfgPolicy).
		WithStatusSubresource(cfgPolicy).
		Build()

	nsSelUpdatesChan := make(chan event.GenericEvent, 20)
	nsSelReconciler := common.NewNamespaceSelectorReconciler(runtimeClient, nsSelUpdatesChan)

	defaultNs := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]interface{}{
				"name": "default",
			},
		},
	}

	// Create default namespace
	if _, err := dynamicClient.Resource(schema.
		GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}).
		Create(ctx, defaultNs, metav1.CreateOptions{}); err != nil && !k8serrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("unable to create default namespace: %w", err)
	}

	// Create default namespace for namespace selector
	err := runtimeClient.Create(ctx, defaultNs)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return nil, err
	}

	rec := ctrl.ConfigurationPolicyReconciler{
		Client:                 runtimeClient,
		DecryptionConcurrency:  1,
		DynamicWatcher:         dynamicWatcher,
		Scheme:                 scheme.Scheme,
		Recorder:               record.NewFakeRecorder(8),
		InstanceName:           "policy-cli",
		TargetK8sClient:        clientset,
		TargetK8sDynamicClient: dynamicClient,
		SelectorReconciler:     &nsSelReconciler,
		EnableMetrics:          false,
		UninstallMode:          false,
		EvalBackoffSeconds:     5,
		FullDiffs:              d.fullDiffs,
	}

	if d.mappingsPath != "" {
		mFile, err := os.ReadFile(d.mappingsPath)
		if err != nil {
			return nil, err
		}

		apiMappings := []mappings.APIMapping{}
		if err := k8syaml.Unmarshal(mFile, &apiMappings); err != nil {
			return nil, err
		}

		clientset.Resources = mappings.ResourceLists(apiMappings)
	} else {
		clientset.Resources, err = mappings.DefaultResourceLists()
		if err != nil {
			return nil, err
		}
	}

	// Add open-cluster-management policy CRD
	addSupportedResources(clientset)

	// wait for dynamic watcher to have started
	<-rec.DynamicWatcher.Started()

	return &rec, nil
}

func (d *DryRunner) compareStatus(cmd *cobra.Command, status policyv1.ConfigurationPolicyStatus) error {
	reader, err := os.Open(d.desiredStatus)
	if err != nil {
		return err
	}

	configBytes, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	inputMap := map[string]interface{}{}

	if err := yaml.Unmarshal(configBytes, &inputMap); err != nil {
		return err
	}

	resultMap := toMap(status)
	if resultMap == nil {
		return errors.New("unable to convert ConfigurationPolicyStatus to unstructured")
	}

	compareStatus(cmd, inputMap, resultMap, d.noColors)

	return nil
}

func (d *DryRunner) saveStatus(status policyv1.ConfigurationPolicyStatus) error {
	f, err := os.Create(d.statusPath)
	if err != nil {
		return err
	}

	out, err := k8syaml.Marshal(status)
	if err != nil {
		return err
	}

	fmt.Fprint(f, string(out))

	return nil
}

func (d *DryRunner) saveOrPrintComplianceMessages(
	ctx context.Context, cmd *cobra.Command, rec client.Client, ns string,
) error {
	events := corev1.EventList{}

	if err := rec.List(ctx, &events, client.InNamespace(ns)); err != nil {
		return err
	}

	messages := []string{}

	for _, ev := range events.Items {
		if ev.InvolvedObject.Name == parentName {
			messages = append(messages, ev.Message)
		}
	}

	if d.messagesPath != "" {
		f, err := os.Create(d.messagesPath)
		if err != nil {
			return err
		}

		for _, msg := range messages {
			fmt.Fprintln(f, msg)
		}
	} else {
		cmd.Println("# Compliance messages:")

		for _, msg := range messages {
			cmd.Println(msg)
		}
	}

	return nil
}

func (d *DryRunner) outputDiffs(cmd *cobra.Command, status policyv1.ConfigurationPolicyStatus) {
	cmd.Println("# Diffs:")

	for _, relObj := range status.RelatedObjects {
		obj := relObj.Object

		name := obj.Metadata.Name
		if obj.Metadata.Namespace != "" {
			name = obj.Metadata.Namespace + "/" + name
		}

		cmd.Printf("%v %v %v:\n", obj.APIVersion, obj.Kind, name)

		if relObj.Properties == nil || relObj.Properties.Diff == "" {
			cmd.Println() // Ensures a newline is printed
		} else {
			// For long diff
			cmd.Println(strings.TrimSuffix(addColorToDiff(relObj.Properties.Diff, d.noColors), "\n"))
		}
	}
}

func sanitizeForCreation(obj *unstructured.Unstructured) {
	// Remove fields that should not be set during creation
	delete(obj.Object["metadata"].(map[string]interface{}), "resourceVersion")
	delete(obj.Object["metadata"].(map[string]interface{}), "generatedName")
	delete(obj.Object["metadata"].(map[string]interface{}), "creationTimestamp")
	delete(obj.Object["metadata"].(map[string]interface{}), "selfLink")
	delete(obj.Object["metadata"].(map[string]interface{}), "uid")
}

func addSupportedResources(clientset *clientsetfake.Clientset) {
	clientset.Resources = append(clientset.Resources, &metav1.APIResourceList{
		GroupVersion: parentpolicyv1.GroupVersion.String(),
		APIResources: []metav1.APIResource{{
			Name:         "policies",
			SingularName: "policy",
			Group:        parentpolicyv1.GroupVersion.Group,
			Version:      parentpolicyv1.GroupVersion.Version,
			Namespaced:   true,
			Kind:         parentpolicyv1.Kind,
			Verbs:        mappings.DefaultVerbs,
		}, {
			Name:         "configurationpolicies",
			SingularName: "configurationpolicy",
			Group:        policyv1.GroupVersion.Group,
			Version:      policyv1.GroupVersion.Version,
			Namespaced:   true,
			Kind:         "ConfigurationPolicy",
			Verbs:        mappings.DefaultVerbs,
		}},
	})
}
