// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/open-cluster-management/addon-framework/pkg/lease"
	"github.com/spf13/pflag"

	policyv1 "github.com/open-cluster-management/config-policy-controller/api/v1"
	"github.com/open-cluster-management/config-policy-controller/controllers"
	"github.com/open-cluster-management/config-policy-controller/pkg/common"
	"github.com/open-cluster-management/config-policy-controller/version"
	//+kubebuilder:scaffold:imports
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost       = "0.0.0.0"
	metricsPort int32 = 8383
	scheme            = k8sruntime.NewScheme()
	log               = ctrl.Log.WithName("setup")
)

func printVersion() {
	log.Info("Using", "OperatorVersion", version.Version, "GoVersion", runtime.Version(),
		"GOOS", runtime.GOOS, "GOARCH", runtime.GOARCH)
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(policyv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

}

func main() {
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	var eventOnParent, clusterName, hubConfigSecretNs, hubConfigSecretName string
	var frequency uint
	var enableLease bool
	pflag.UintVar(&frequency, "update-frequency", 10,
		"The status update frequency (in seconds) of a mutation policy")
	pflag.StringVar(&eventOnParent, "parent-event", "ifpresent",
		"to also send status events on parent policy. options are: yes/no/ifpresent")
	pflag.BoolVar(&enableLease, "enable-lease", false,
		"If enabled, the controller will start the lease controller to report its status")
	pflag.StringVar(&clusterName, "cluster-name", "acm-managed-cluster", "Name of the cluster")
	pflag.StringVar(&hubConfigSecretNs, "hubconfig-secret-ns", "open-cluster-management-agent-addon", "Namespace for hub config kube-secret")
	pflag.StringVar(&hubConfigSecretName, "hubconfig-secret-name", "policy-controller-hub-kubeconfig", "Name of the hub config kube-secret")

	var enableLeaderElection, legacyLeaderElection bool
	var probeAddr string
	pflag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	pflag.BoolVar(&enableLeaderElection, "leader-elect", true,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	pflag.BoolVar(&legacyLeaderElection, "legacy-leader-elect", false,
		"Use a legacy leader election method for controller manager instead of the lease API.")

	pflag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	printVersion()

	namespace, err := common.GetWatchNamespace()
	if err != nil {
		log.Error(err, "Failed to get watch namespace")
		os.Exit(1)
	}

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "Failed to get config")
		os.Exit(1)
	}

	// Set default manager options
	options := manager.Options{
		Namespace:              namespace,
		MetricsBindAddress:     fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		Scheme:                 scheme,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "config-policy-controller.open-cluster-management.io",
	}

	if strings.Contains(namespace, ",") {
		options.Namespace = ""
		options.NewCache = cache.MultiNamespacedCacheBuilder(strings.Split(namespace, ","))
	}

	if legacyLeaderElection {
		// If legacyLeaderElection is enabled, then that means the lease API is not available.
		// In this case, use the legacy leader election method of a ConfigMap.
		options.LeaderElectionResourceLock = "configmaps"
	}

	// Create a new manager to provide shared dependencies and start components
	mgr, err := manager.New(cfg, options)
	if err != nil {
		log.Error(err, "Unable to start manager")
		os.Exit(1)
	}

	reconciler := controllers.ConfigurationPolicyReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor(controllers.ControllerName),
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

	// Initialize some variables
	clientset := kubernetes.NewForConfigOrDie(cfg)
	common.Initialize(clientset, cfg)
	controllers.Initialize(cfg, clientset, namespace, eventOnParent)

	// PeriodicallyExecConfigPolicies is the go-routine that periodically checks the policies
	go reconciler.PeriodicallyExecConfigPolicies(frequency, false)

	// This lease is not related to leader election. This is to report the status of the controller
	// to the addon framework. This can be seen in the "status" section of the ManagedClusterAddOn
	// resource objects.
	if enableLease {
		operatorNs, err := common.GetOperatorNamespace()
		log.V(2).Info("Got operator namespace", "Namespace", operatorNs)
		if err != nil {
			if err == common.ErrNoNamespace || err == common.ErrRunLocal {
				log.Info("Skipping lease; not running in a cluster")
			} else {
				log.Error(err, "Failed to get operator namespace")
				os.Exit(1)
			}
		} else {

			log.Info("Starting lease controller to report status")
			leaseUpdater := lease.NewLeaseUpdater(
				clientset,
				"config-policy-controller",
				operatorNs,
			)

			//set hubCfg on lease updated if found
			hubCfg, err := common.LoadHubConfig(hubConfigSecretNs, hubConfigSecretName)
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
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "Problem running manager")
		os.Exit(1)
	}
}
