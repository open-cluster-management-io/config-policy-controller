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
	"sync"
	"time"

	"github.com/go-logr/zapr"
	operatorv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/spf13/pflag"
	"github.com/stolostron/go-log-utils/zaputil"
	depclient "github.com/stolostron/kubernetes-dependency-watches/client"
	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	extensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
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
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	policyv1beta1 "open-cluster-management.io/config-policy-controller/api/v1beta1"
	"open-cluster-management.io/config-policy-controller/controllers"
	"open-cluster-management.io/config-policy-controller/pkg/common"
	"open-cluster-management.io/config-policy-controller/pkg/triggeruninstall"
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
	utilruntime.Must(policyv1beta1.AddToScheme(scheme))
	utilruntime.Must(extensionsv1.AddToScheme(scheme))
	utilruntime.Must(extensionsv1beta1.AddToScheme(scheme))
	utilruntime.Must(operatorv1alpha1.AddToScheme(scheme))
	utilruntime.Must(operatorv1.AddToScheme(scheme))
}

type ctrlOpts struct {
	clusterName           string
	hubConfigPath         string
	targetKubeConfig      string
	metricsAddr           string
	probeAddr             string
	operatorPolDefaultNS  string
	clientQPS             float32
	clientBurst           uint
	frequency             uint
	decryptionConcurrency uint8
	evaluationConcurrency uint8
	enableLease           bool
	enableLeaderElection  bool
	enableMetrics         bool
	enableOperatorPolicy  bool
}

func main() {
	klog.InitFlags(nil)

	subcommand := ""
	if len(os.Args) >= 2 {
		subcommand = os.Args[1]
	}

	switch subcommand {
	case "controller":
		break // normal mode - just continue execution
	case "trigger-uninstall":
		handleTriggerUninstall()

		return
	default:
		fmt.Fprintln(os.Stderr, "expected 'controller' or 'trigger-uninstall' subcommands")
		os.Exit(1)
	}

	zflags := zaputil.FlagConfig{
		LevelName:   "log-level",
		EncoderName: "log-encoder",
	}

	controllerFlagSet := pflag.NewFlagSet("controller", pflag.ExitOnError)

	zflags.Bind(flag.CommandLine)
	controllerFlagSet.AddGoFlagSet(flag.CommandLine)

	opts := parseOpts(controllerFlagSet, os.Args[2:])

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

	if opts.evaluationConcurrency < 1 {
		panic("The --evaluation-concurrency option cannot be less than 1")
	}

	printVersion()

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "Failed to get config")
		os.Exit(1)
	}

	cfg.Burst = int(opts.clientBurst)
	cfg.QPS = opts.clientQPS

	nsTransform := func(obj interface{}) (interface{}, error) {
		ns := obj.(*corev1.Namespace)
		guttedNS := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   ns.Name,
				Labels: ns.Labels,
			},
		}

		return guttedNS, nil
	}

	// Set a field selector so that a watch on CRDs will be limited to just the configuration policy CRD.
	cacheByObject := map[client.Object]cache.ByObject{
		&extensionsv1.CustomResourceDefinition{}: {
			Field: fields.SelectorFromSet(fields.Set{"metadata.name": controllers.CRDName}),
		},
		&extensionsv1beta1.CustomResourceDefinition{}: {
			Field: fields.SelectorFromSet(fields.Set{"metadata.name": controllers.CRDName}),
		},
		&corev1.Namespace{}: {
			Transform: nsTransform,
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
		cacheByObject[&appsv1.Deployment{}] = cache.ByObject{
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
		cacheByObject[&policyv1.ConfigurationPolicy{}] = cache.ByObject{
			Field: fields.SelectorFromSet(fields.Set{
				"metadata.namespace": watchNamespace,
			}),
		}
		cacheByObject[&policyv1beta1.OperatorPolicy{}] = cache.ByObject{
			Field: fields.SelectorFromSet(fields.Set{
				"metadata.namespace": watchNamespace,
			}),
		}
	} else {
		log.Info("Skipping restrictions on the ConfigurationPolicy cache because watchNamespace is empty")
	}

	// Set default manager options
	options := manager.Options{
		Metrics: server.Options{
			BindAddress: opts.metricsAddr,
		},
		Scheme: scheme,
		WebhookServer: webhook.NewServer(
			webhook.Options{
				Port: 9443,
			},
		),
		HealthProbeBindAddress: opts.probeAddr,
		LeaderElection:         opts.enableLeaderElection,
		LeaderElectionID:       "config-policy-controller.open-cluster-management.io",
		Cache: cache.Options{
			ByObject: cacheByObject,
		},
		// Disable the cache for Secrets to avoid a watch getting created when the `policy-encryption-key`
		// Secret is retrieved. Special cache handling is done by the controller.
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{&corev1.Secret{}},
			},
		},
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

	// Create a new manager to provide shared dependencies and start components
	mgr, err := manager.New(cfg, options)
	if err != nil {
		log.Error(err, "Unable to start manager")
		os.Exit(1)
	}

	terminatingCtx := ctrl.SetupSignalHandler()

	uninstallingCtx, uninstallingCtxCancel := context.WithCancel(terminatingCtx)

	var beingUninstalled bool

	// Can't use the manager client because the manager isn't started yet.
	uninstallCheckClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		log.Error(err, "Failed to determine if the controller is being uninstalled at startup. Will assume it's not.")
	} else {
		beingUninstalled, err = controllers.IsBeingUninstalled(uninstallCheckClient)
		if err != nil {
			log.Error(
				err,
				"Failed to determine if the controller is being uninstalled at startup. Will assume it's not.",
			)
		}
	}

	if beingUninstalled {
		log.Info("The controller is being uninstalled. Will enter uninstall mode.")

		uninstallingCtxCancel()
	} else {
		log.V(2).Info("The controller is not being uninstalled. Will continue as normal.")
	}

	var targetK8sClient kubernetes.Interface
	var targetK8sDynamicClient dynamic.Interface
	var targetK8sConfig *rest.Config
	var targetClient client.Client
	var nsSelMgr manager.Manager // A separate controller-manager is needed in hosted mode

	if opts.targetKubeConfig == "" {
		targetK8sConfig = cfg
		targetK8sClient = kubernetes.NewForConfigOrDie(targetK8sConfig)
		targetK8sDynamicClient = dynamic.NewForConfigOrDie(targetK8sConfig)
		nsSelMgr = mgr
		targetClient = mgr.GetClient()
	} else { // "Hosted mode"
		var err error

		targetK8sConfig, err = clientcmd.BuildConfigFromFlags("", opts.targetKubeConfig)
		if err != nil {
			log.Error(err, "Failed to load the target kubeconfig", "path", opts.targetKubeConfig)
			os.Exit(1)
		}

		targetK8sConfig.Burst = int(opts.clientBurst)
		targetK8sConfig.QPS = opts.clientQPS

		targetK8sClient = kubernetes.NewForConfigOrDie(targetK8sConfig)
		targetK8sDynamicClient = dynamic.NewForConfigOrDie(targetK8sConfig)
		targetClient, err = client.New(targetK8sConfig, client.Options{Scheme: scheme})
		if err != nil {
			log.Error(err, "Failed to load the target kubeconfig", "path", opts.targetKubeConfig)
			os.Exit(1)
		}

		// The managed cluster's API server is potentially not the same as the hosting cluster and it could be
		// offline already as part of the uninstall process. In this case, the manager's instantiation will fail.
		// This controller is not needed in uninstall mode, so just skip it.
		if !beingUninstalled {
			nsSelMgr, err = manager.New(targetK8sConfig, manager.Options{
				Cache: cache.Options{
					ByObject: map[client.Object]cache.ByObject{
						&corev1.Namespace{}: {
							Transform: nsTransform,
						},
					},
				},
			})
			if err != nil {
				log.Error(err, "Unable to create manager from target kube config")
				os.Exit(1)
			}
		}

		log.Info(
			"Overrode the target Kubernetes cluster for policy evaluation and enforcement",
			"path", opts.targetKubeConfig,
		)
	}

	instanceName, _ := os.Hostname() // on an error, instanceName will be empty, which is ok

	var nsSelReconciler common.NamespaceSelectorReconciler
	var dryRunSupported bool

	if !beingUninstalled {
		nsSelReconciler = common.NamespaceSelectorReconciler{
			Client: nsSelMgr.GetClient(),
		}
		if err = nsSelReconciler.SetupWithManager(nsSelMgr); err != nil {
			log.Error(err, "Unable to create controller", "controller", "NamespaceSelector")
			os.Exit(1)
		}

		discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(targetK8sConfig)

		serverVersion, err := discoveryClient.ServerVersion()
		if err != nil {
			log.Error(err, "unable to detect the managed cluster's Kubernetes version")
			os.Exit(1)
		}

		dryRunSupported = semver.Compare(serverVersion.GitVersion, "v1.18.0") >= 0
		if dryRunSupported {
			log.Info("The managed cluster supports dry run API requests")
		} else {
			log.Info(
				"The managed cluster does not support dry run API requests. Will assume that empty values are equal " +
					"to not being set.",
			)
		}
	}

	reconciler := controllers.ConfigurationPolicyReconciler{
		Client:                 mgr.GetClient(),
		DecryptionConcurrency:  opts.decryptionConcurrency,
		DryRunSupported:        dryRunSupported,
		EvaluationConcurrency:  opts.evaluationConcurrency,
		Scheme:                 mgr.GetScheme(),
		Recorder:               mgr.GetEventRecorderFor(controllers.ControllerName),
		InstanceName:           instanceName,
		TargetK8sClient:        targetK8sClient,
		TargetK8sDynamicClient: targetK8sDynamicClient,
		TargetK8sConfig:        targetK8sConfig,
		SelectorReconciler:     &nsSelReconciler,
		EnableMetrics:          opts.enableMetrics,
		UninstallMode:          beingUninstalled,
	}

	managerCtx, managerCancel := context.WithCancel(context.Background())

	if err = reconciler.SetupWithManager(mgr); err != nil {
		log.Error(err, "Unable to create controller", "controller", "ConfigurationPolicy")
		os.Exit(1)
	}

	if opts.enableOperatorPolicy {
		depReconciler, depEvents := depclient.NewControllerRuntimeSource()

		watcher, err := depclient.New(targetK8sConfig, depReconciler,
			&depclient.Options{
				DisableInitialReconcile: true,
				EnableCache:             true,
				ObjectCacheOptions:      depclient.ObjectCacheOptions{UnsafeDisableDeepCopy: false},
			})
		if err != nil {
			log.Error(err, "Unable to create dependency watcher")
			os.Exit(1)
		}

		go func() {
			err := watcher.Start(managerCtx)
			if err != nil {
				panic(err)
			}
		}()

		// Wait until the dynamic watcher has started.
		<-watcher.Started()

		OpReconciler := controllers.OperatorPolicyReconciler{
			Client:           mgr.GetClient(),
			DynamicClient:    targetK8sDynamicClient,
			DynamicWatcher:   watcher,
			InstanceName:     instanceName,
			DefaultNamespace: opts.operatorPolDefaultNS,
			TargetClient:     targetClient,
		}

		if err = OpReconciler.SetupWithManager(mgr, depEvents); err != nil {
			log.Error(err, "Unable to create controller", "controller", "OperatorPolicy")
			os.Exit(1)
		}
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

	// PeriodicallyExecConfigPolicies is the go-routine that periodically checks the policies
	log.Info("Periodically processing Configuration Policies", "frequency", opts.frequency)

	go func() {
		reconciler.PeriodicallyExecConfigPolicies(terminatingCtx, opts.frequency, mgr.Elected(), uninstallingCtxCancel)
		managerCancel()
	}()

	// This lease is not related to leader election. This is to report the status of the controller
	// to the addon framework. This can be seen in the "status" section of the ManagedClusterAddOn
	// resource objects.
	if opts.enableLease {
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

			hubCfg, err := clientcmd.BuildConfigFromFlags("", opts.hubConfigPath)
			if err != nil {
				log.Error(err, "Could not load hub config, lease updater not set with config")
			} else {
				leaseUpdater = leaseUpdater.WithHubLeaseConfig(hubCfg, opts.clusterName)
			}

			go leaseUpdater.Start(context.TODO())
		}
	} else {
		log.Info("Addon status reporting is not enabled")
	}

	log.Info("Starting managers")

	var wg sync.WaitGroup
	var errorExit bool

	wg.Add(1)

	go func() {
		if err := mgr.Start(managerCtx); err != nil {
			log.Error(err, "Problem running manager")

			managerCancel()

			errorExit = true
		}

		wg.Done()
	}()

	if !beingUninstalled && opts.targetKubeConfig != "" { // "hosted mode"
		wg.Add(1)

		go func() {
			// Use the uninstallingCtx so that this shuts down when the controller is being uninstalled. This is
			// important since the managed cluster's API server may become unavailable at this time when in hosted mdoe.
			if err := nsSelMgr.Start(uninstallingCtx); err != nil {
				log.Error(err, "Problem running manager")

				managerCancel()

				errorExit = true
			}

			wg.Done()
		}()
	}

	wg.Wait()

	if errorExit {
		os.Exit(1)
	}
}

func handleTriggerUninstall() {
	triggerUninstallFlagSet := pflag.NewFlagSet("trigger-uninstall", pflag.ExitOnError)

	var deploymentName, deploymentNamespace, policyNamespace string
	var timeoutSeconds uint

	triggerUninstallFlagSet.StringVar(
		&deploymentName, "deployment-name", "config-policy-controller", "The name of the controller Deployment object",
	)
	triggerUninstallFlagSet.StringVar(
		&deploymentNamespace,
		"deployment-namespace",
		"open-cluster-management-agent-addon",
		"The namespace of the controller Deployment object",
	)
	triggerUninstallFlagSet.StringVar(
		&policyNamespace, "policy-namespace", "", "The namespace of where ConfigurationPolicy objects are stored",
	)
	triggerUninstallFlagSet.UintVar(
		&timeoutSeconds, "timeout-seconds", 300, "The number of seconds before the operation is canceled",
	)
	triggerUninstallFlagSet.AddGoFlagSet(flag.CommandLine)

	_ = triggerUninstallFlagSet.Parse(os.Args[2:])

	if deploymentName == "" || deploymentNamespace == "" || policyNamespace == "" {
		fmt.Fprintln(os.Stderr, "--deployment-name, --deployment-namespace, --policy-namespace must all have values")
		os.Exit(1)
	}

	if timeoutSeconds < 30 {
		fmt.Fprintln(os.Stderr, "--timeout-seconds must be set to at least 30 seconds")
		os.Exit(1)
	}

	terminatingCtx := ctrl.SetupSignalHandler()
	ctx, cancelCtx := context.WithDeadline(terminatingCtx, time.Now().Add(time.Duration(timeoutSeconds)*time.Second))

	defer cancelCtx()

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		klog.Errorf("Failed to get config: %s", err)
		os.Exit(1)
	}

	err = triggeruninstall.TriggerUninstall(ctx, cfg, deploymentName, deploymentNamespace, policyNamespace)
	if err != nil {
		klog.Errorf("Failed to trigger the uninstall due to the error: %s", err)
		os.Exit(1)
	}
}

func parseOpts(flags *pflag.FlagSet, args []string) *ctrlOpts {
	opts := &ctrlOpts{}

	flags.UintVar(
		&opts.frequency,
		"update-frequency",
		10,
		"The status update frequency (in seconds) of a mutation policy",
	)

	flags.BoolVar(
		&opts.enableLease,
		"enable-lease",
		false,
		"If enabled, the controller will start the lease controller to report its status",
	)

	flags.StringVar(
		&opts.clusterName,
		"cluster-name",
		"acm-managed-cluster",
		"Name of the cluster",
	)

	flags.StringVar(
		&opts.hubConfigPath,
		"hub-kubeconfig-path",
		"/var/run/klusterlet/kubeconfig",
		"Path to the hub kubeconfig",
	)

	flags.StringVar(
		&opts.targetKubeConfig,
		"target-kubeconfig-path",
		"",
		"A path to an alternative kubeconfig for policy evaluation and enforcement.",
	)

	flags.StringVar(
		&opts.metricsAddr,
		"metrics-bind-address",
		"localhost:8383",
		"The address the metrics endpoint binds to.",
	)

	flags.StringVar(
		&opts.probeAddr,
		"health-probe-bind-address",
		":8081",
		"The address the probe endpoint binds to.",
	)

	flags.BoolVar(
		&opts.enableLeaderElection,
		"leader-elect",
		true,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.",
	)

	flags.Uint8Var(
		&opts.decryptionConcurrency,
		"decryption-concurrency",
		5,
		"The max number of concurrent policy template decryptions",
	)

	flags.Uint8Var(
		&opts.evaluationConcurrency,
		"evaluation-concurrency",
		// Set a low default to not add too much load to the Kubernetes API server in resource constrained deployments.
		2,
		"The max number of concurrent configuration policy evaluations",
	)

	flags.BoolVar(
		&opts.enableMetrics,
		"enable-metrics",
		true,
		"Disable custom metrics collection",
	)

	flags.Float32Var(
		&opts.clientQPS,
		"client-max-qps",
		30, // 15 * concurrency is recommended
		"The max queries per second that will be made against the kubernetes API server. "+
			"Will scale with concurrency, if not explicitly set.",
	)

	flags.UintVar(
		&opts.clientBurst,
		"client-burst",
		45, // the controller-runtime defaults are 20:30 (qps:burst) - this matches that ratio
		"The maximum burst before client requests will be throttled. "+
			"Will scale with concurrency, if not explicitly set.",
	)

	flags.BoolVar(
		&opts.enableOperatorPolicy,
		"enable-operator-policy",
		false,
		"Enable operator policy controller",
	)

	flags.StringVar(
		&opts.operatorPolDefaultNS,
		"operator-policy-default-namespace",
		"",
		"The default namespace to be used by an OperatorPolicy if not specified in the policy.",
	)

	_ = flags.Parse(args)

	// Scale QPS and Burst with concurrency, when they aren't explicitly set.
	if flags.Changed("evaluation-concurrency") {
		if !flags.Changed("client-max-qps") {
			opts.clientQPS = float32(opts.evaluationConcurrency) * 15
		}

		if !flags.Changed("client-burst") {
			opts.clientBurst = uint(opts.evaluationConcurrency)*22 + 1
		}
	}

	return opts
}
