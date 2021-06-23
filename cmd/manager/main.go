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

	"github.com/open-cluster-management/addon-framework/pkg/lease"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/open-cluster-management/config-policy-controller/pkg/apis"
	"github.com/open-cluster-management/config-policy-controller/pkg/common"
	"github.com/open-cluster-management/config-policy-controller/pkg/controller"
	policyStatusHandler "github.com/open-cluster-management/config-policy-controller/pkg/controller/configurationpolicy"
	"github.com/open-cluster-management/config-policy-controller/version"

	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	"github.com/operator-framework/operator-sdk/pkg/leader"
	sdkVersion "github.com/operator-framework/operator-sdk/version"
	"github.com/spf13/pflag"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost       = "0.0.0.0"
	metricsPort int32 = 8383
)
var log = logf.Log.WithName("cmd")

func printVersion() {
	log.Info(fmt.Sprintf("Operator Version: %s", version.Version))
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	log.Info(fmt.Sprintf("Version of operator-sdk: %v", sdkVersion.Version))
}

func main() {
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

	pflag.Parse()

	logf.SetLogger(zap.New())

	printVersion()

	namespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		log.Error(err, "Failed to get watch namespace")
		os.Exit(1)
	}

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	ctx := context.TODO()
	// Become the leader before proceeding
	err = leader.Become(ctx, "config-policy-ctrl-lock")
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Set default manager options
	options := manager.Options{
		Namespace:          namespace,
		MetricsBindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
	}

	if strings.Contains(namespace, ",") {
		options.Namespace = ""
		options.NewCache = cache.MultiNamespacedCacheBuilder(strings.Split(namespace, ","))
	}

	// Create a new manager to provide shared dependencies and start components
	mgr, err := manager.New(cfg, options)
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Setup all Controllers
	if err := controller.AddToManager(mgr); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Initialize some variables
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Info("cannot create kube client sucessfully: %v", err)
	}
	var generatedClient kubernetes.Interface = kubernetes.NewForConfigOrDie(mgr.GetConfig())
	common.Initialize(&generatedClient, cfg)

	policyStatusHandler.Initialize(cfg, client, &generatedClient, mgr, namespace, eventOnParent)
	// PeriodicallyExecConfigPolicies is the go-routine that periodically checks the policies
	go policyStatusHandler.PeriodicallyExecConfigPolicies(frequency, false)

	if enableLease {
		operatorNs, err := k8sutil.GetOperatorNamespace()
		if err != nil {
			if err == k8sutil.ErrNoNamespace || err == k8sutil.ErrRunLocal {
				log.Info("Skipping lease; not running in a cluster.")
			} else {
				log.Error(err, "Failed to get operator namespace")
				os.Exit(1)
			}
		} else {

			log.Info("Starting lease controller to report status")
			leaseUpdater := lease.NewLeaseUpdater(
				generatedClient,
				"config-policy-controller",
				operatorNs,
			)

			//set hubCfg on lease updated if found
			hubCfg, _ := common.LoadHubConfig(hubConfigSecretNs, hubConfigSecretName)
			if hubCfg != nil {
				leaseUpdater = leaseUpdater.WithHubLeaseConfig(hubCfg, clusterName)
			} else {
				log.Error(err, "HubConfig not found, HubLeaseConfig not set")
			}

			go leaseUpdater.Start(ctx)
		}
	} else {
		log.Info("Status reporting is not enabled")
	}

	log.Info("Starting the Cmd.")

	// Start the Cmd
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "Manager exited non-zero")
		os.Exit(1)
	}
}
