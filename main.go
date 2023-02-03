// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/go-logr/zapr"
	"github.com/spf13/pflag"
	"github.com/stolostron/go-log-utils/zaputil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	extensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/fields"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	// Import all k8s client auth plugins to ensure that exec-entrypoint and run can use them
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/lease"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	"open-cluster-management.io/config-policy-controller/controllers"
	"open-cluster-management.io/config-policy-controller/pkg/common"
	"open-cluster-management.io/config-policy-controller/version"
)

// Change below variables to serve metrics on different host or port.
var (
	scheme = k8sruntime.NewScheme()
	log    = ctrl.Log.WithName("setup")
)

func printVersion() {
	log.Info("Using", "OperatorVersion", version.Version, "GoVersion", runtime.Version(),
		"GOOS", runtime.GOOS, "GOARCH", runtime.GOARCH)
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
	utilruntime.Must(policyv1.AddToScheme(scheme))
	utilruntime.Must(extensionsv1.AddToScheme(scheme))
	utilruntime.Must(extensionsv1beta1.AddToScheme(scheme))
}

func main() {
	klog.InitFlags(nil)

	zflags := zaputil.FlagConfig{
		LevelName:   "log-level",
		EncoderName: "log-encoder",
	}

	zflags.Bind(flag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	var clusterName, hubConfigPath, targetKubeConfig, metricsAddr, probeAddr string
	var frequency uint
	var decryptionConcurrency, evaluationConcurrency uint8
	var enableLease, enableLeaderElection, legacyLeaderElection, enableMetrics bool

	pflag.UintVar(&frequency, "update-frequency", 10,
		"The status update frequency (in seconds) of a mutation policy")
	pflag.BoolVar(&enableLease, "enable-lease", false,
		"If enabled, the controller will start the lease controller to report its status")
	pflag.StringVar(&clusterName, "cluster-name", "acm-managed-cluster", "Name of the cluster")
	pflag.StringVar(&hubConfigPath, "hub-kubeconfig-path", "/var/run/klusterlet/kubeconfig",
		"Path to the hub kubeconfig")
	pflag.StringVar(
		&targetKubeConfig,
		"target-kubeconfig-path",
		"",
		"A path to an alternative kubeconfig for policy evaluation and enforcement.",
	)
	pflag.StringVar(
		&metricsAddr, "metrics-bind-address", "localhost:8383", "The address the metrics endpoint binds to.",
	)
	pflag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	pflag.BoolVar(&enableLeaderElection, "leader-elect", true,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	pflag.BoolVar(&legacyLeaderElection, "legacy-leader-elect", false,
		"Use a legacy leader election method for controller manager instead of the lease API.")
	pflag.Uint8Var(
		&decryptionConcurrency,
		"decryption-concurrency",
		5,
		"The max number of concurrent policy template decryptions",
	)
	pflag.Uint8Var(
		&evaluationConcurrency,
		"evaluation-concurrency",
		// Set a low default to not add too much load to the Kubernetes API server in resource constrained deployments.
		2,
		"The max number of concurrent configuration policy evaluations",
	)
	pflag.BoolVar(&enableMetrics, "enable-metrics", true, "Disable custom metrics collection")

	pflag.Parse()

	ctrlZap, err := zflags.BuildForCtrl()
	if err != nil {
		panic(fmt.Sprintf("Failed to build zap logger for controller: %v", err))
	}

	ctrl.SetLogger(zapr.NewLogger(ctrlZap))

	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	err = zaputil.SyncWithGlogFlags(klogFlags)
	if err != nil {
		log.Error(err, "Failed to synchronize klog and glog flags, continuing with what succeeded")
	}

	klogZap, err := zaputil.BuildForKlog(zflags.GetConfig(), klogFlags)
	if err != nil {
		log.Error(err, "Failed to build zap logger for klog, those logs will not go through zap")
	} else {
		klog.SetLogger(zapr.NewLogger(klogZap).WithName("klog"))
	}

	if evaluationConcurrency < 1 {
		panic("The --evaluation-concurrency option cannot be less than 1")
	}

	printVersion()

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "Failed to get config")
		os.Exit(1)
	}

	// Set a field selector so that a watch on CRDs will be limited to just the configuration policy CRD.
	cacheSelectors := cache.SelectorsByObject{
		&extensionsv1.CustomResourceDefinition{}: {
			Field: fields.SelectorFromSet(fields.Set{"metadata.name": controllers.CRDName}),
		},
		&extensionsv1beta1.CustomResourceDefinition{}: {
			Field: fields.SelectorFromSet(fields.Set{"metadata.name": controllers.CRDName}),
		},
	}

	ctrlKey, err := common.GetOperatorNamespacedName()
	if err != nil {
		if errors.Is(err, common.ErrNoNamespace) || errors.Is(err, common.ErrRunLocal) {
			log.Info("Running locally, skipping restrictions on the Deployment cache")
		} else {
			log.Error(err, "Failed to identify the controller's deployment")
			os.Exit(1)
		}
	} else {
		cacheSelectors[&appsv1.Deployment{}] = cache.ObjectSelector{
			Field: fields.SelectorFromSet(fields.Set{
				"metadata.namespace": ctrlKey.Namespace,
				"metadata.name":      ctrlKey.Name,
			}),
		}
	}

	watchNamespace, err := common.GetWatchNamespace()
	if err != nil {
		log.Error(err, "Failed to get watch namespace")
		os.Exit(1)
	}

	if strings.Contains(watchNamespace, ",") {
		err = fmt.Errorf("multiple watched namespaces are not allowed for this controller")
		log.Error(err, "Failed to get watch namespace")
		os.Exit(1)
	}

	log.V(2).Info("Configured the watch namespace", "watchNamespace", watchNamespace)

	if watchNamespace != "" {
		cacheSelectors[&policyv1.ConfigurationPolicy{}] = cache.ObjectSelector{
			Field: fields.SelectorFromSet(fields.Set{
				"metadata.namespace": watchNamespace,
			}),
		}
	} else {
		log.Info("Skipping restrictions on the ConfigurationPolicy cache because watchNamespace is empty")
	}

	// Set default manager options
	options := manager.Options{
		MetricsBindAddress:     metricsAddr,
		Scheme:                 scheme,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "config-policy-controller.open-cluster-management.io",
		NewCache:               cache.BuilderWithOptions(cache.Options{SelectorsByObject: cacheSelectors}),
		// Disable the cache for Secrets to avoid a watch getting created when the `policy-encryption-key`
		// Secret is retrieved. Special cache handling is done by the controller.
		ClientDisableCacheFor: []client.Object{&corev1.Secret{}},
		// Override the EventBroadcaster so that the spam filter will not ignore events for the policy but with
		// different messages if a large amount of events for that policy are sent in a short time.
		EventBroadcaster: record.NewBroadcasterWithCorrelatorOptions(
			record.CorrelatorOptions{
				// This essentially disables event aggregation of the same events but with different messages.
				MaxIntervalInSeconds: 1,
				// This is the default spam key function except it adds the reason and message as well.
				// https://github.com/kubernetes/client-go/blob/v0.23.3/tools/record/events_cache.go#L70-L82
				SpamKeyFunc: func(event *corev1.Event) string {
					return strings.Join(
						[]string{
							event.Source.Component,
							event.Source.Host,
							event.InvolvedObject.Kind,
							event.InvolvedObject.Namespace,
							event.InvolvedObject.Name,
							string(event.InvolvedObject.UID),
							event.InvolvedObject.APIVersion,
							event.Reason,
							event.Message,
						},
						"",
					)
				},
			},
		),
	}

	if legacyLeaderElection {
		// If legacyLeaderElection is enabled, then that means the lease API is not available.
		// In this case, use the legacy leader election method of a ConfigMap.
		log.Info("Using the legacy leader election of configmaps")

		options.LeaderElectionResourceLock = "configmaps"
	} else {
		// use the leases leader election by default for controller-runtime 0.11 instead of
		// the default of configmapsleases (leases is the new default in 0.12)
		options.LeaderElectionResourceLock = "leases"
	}

	// Create a new manager to provide shared dependencies and start components
	mgr, err := manager.New(cfg, options)
	if err != nil {
		log.Error(err, "Unable to start manager")
		os.Exit(1)
	}

	var targetK8sClient kubernetes.Interface
	var targetK8sDynamicClient dynamic.Interface
	var targetK8sConfig *rest.Config

	if targetKubeConfig == "" {
		targetK8sConfig = cfg
		targetK8sClient = kubernetes.NewForConfigOrDie(targetK8sConfig)
		targetK8sDynamicClient = dynamic.NewForConfigOrDie(targetK8sConfig)
	} else {
		var err error

		targetK8sConfig, err = clientcmd.BuildConfigFromFlags("", targetKubeConfig)
		if err != nil {
			log.Error(err, "Failed to load the target kubeconfig", "path", targetKubeConfig)
			os.Exit(1)
		}

		targetK8sClient = kubernetes.NewForConfigOrDie(targetK8sConfig)
		targetK8sDynamicClient = dynamic.NewForConfigOrDie(targetK8sConfig)

		log.Info(
			"Overrode the target Kubernetes cluster for policy evaluation and enforcement", "path", targetKubeConfig,
		)
	}

	instanceName, _ := os.Hostname() // on an error, instanceName will be empty, which is ok

	reconciler := controllers.ConfigurationPolicyReconciler{
		Client:                 mgr.GetClient(),
		DecryptionConcurrency:  decryptionConcurrency,
		EvaluationConcurrency:  evaluationConcurrency,
		Scheme:                 mgr.GetScheme(),
		Recorder:               mgr.GetEventRecorderFor(controllers.ControllerName),
		InstanceName:           instanceName,
		TargetK8sClient:        targetK8sClient,
		TargetK8sDynamicClient: targetK8sDynamicClient,
		TargetK8sConfig:        targetK8sConfig,
		EnableMetrics:          enableMetrics,
	}
	if err = reconciler.SetupWithManager(mgr); err != nil {
		log.Error(err, "Unable to create controller", "controller", "ConfigurationPolicy")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "Unable to set up health check")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "Unable to set up ready check")
		os.Exit(1)
	}

	terminatingCtx := ctrl.SetupSignalHandler()
	managerCtx, managerCancel := context.WithCancel(context.Background())

	// PeriodicallyExecConfigPolicies is the go-routine that periodically checks the policies
	log.V(1).Info("Perodically processing Configuration Policies", "frequency", frequency)

	go func() {
		reconciler.PeriodicallyExecConfigPolicies(terminatingCtx, frequency, mgr.Elected())
		managerCancel()
	}()

	// This lease is not related to leader election. This is to report the status of the controller
	// to the addon framework. This can be seen in the "status" section of the ManagedClusterAddOn
	// resource objects.
	if enableLease {
		operatorNs, err := common.GetOperatorNamespace()
		if err != nil {
			if errors.Is(err, common.ErrNoNamespace) || errors.Is(err, common.ErrRunLocal) {
				log.Info("Skipping lease; not running in a cluster")
			} else {
				log.Error(err, "Failed to get operator namespace")
				os.Exit(1)
			}
		} else {
			log.V(2).Info("Got operator namespace", "Namespace", operatorNs)
			log.Info("Starting lease controller to report status")

			leaseUpdater := lease.NewLeaseUpdater(
				// Always use the cluster that is running the controller for the lease.
				kubernetes.NewForConfigOrDie(cfg), "config-policy-controller", operatorNs,
			)

			hubCfg, err := clientcmd.BuildConfigFromFlags("", hubConfigPath)
			if err != nil {
				log.Error(err, "Could not load hub config, lease updater not set with config")
			} else {
				leaseUpdater = leaseUpdater.WithHubLeaseConfig(hubCfg, clusterName)
			}

			go leaseUpdater.Start(context.TODO())
		}
	} else {
		log.Info("Addon status reporting is not enabled")
	}

	log.Info("Starting manager")

	if err := mgr.Start(managerCtx); err != nil {
		log.Error(err, "Problem running manager")
		os.Exit(1)
	}
}
