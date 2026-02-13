/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/md5"
	"crypto/tls"
	"flag"
	"fmt"
	"os"

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/flags"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	capictrl "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	bootstrapv1beta1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	bootstrapv1beta2 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	cpv1beta2 "github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	infrastructurev1beta1 "github.com/k0sproject/k0smotron/api/infrastructure/v1beta1"
	k0smotronv1beta1 "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	"github.com/k0sproject/k0smotron/internal/controller/bootstrap"
	"github.com/k0sproject/k0smotron/internal/controller/controlplane"
	"github.com/k0sproject/k0smotron/internal/controller/infrastructure"
	controller "github.com/k0sproject/k0smotron/internal/controller/k0smotron.io"
	"github.com/k0sproject/k0smotron/internal/featuregate"
	//+kubebuilder:scaffold:imports
)

var (
	scheme             = runtime.NewScheme()
	setupLog           = ctrl.Log.WithName("setup")
	enabledControllers = map[string]bool{
		bootstrapController:      true,
		controlPlaneController:   true,
		infrastructureController: true,
	}
	featureGates   string
	managerOptions = flags.ManagerOptions{}
)

const (
	allControllers = "all"
	// CAPI controllers flags
	bootstrapController      = "bootstrap"
	controlPlaneController   = "control-plane"
	infrastructureController = "infrastructure"
	// Standalone controller flag
	standaloneController = "standalone"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(k0smotronv1beta1.AddToScheme(scheme))
	utilruntime.Must(bootstrapv1beta1.AddToScheme(scheme))
	utilruntime.Must(bootstrapv1beta2.AddToScheme(scheme))

	// Register cluster-api types
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(cpv1beta1.AddToScheme(scheme))
	utilruntime.Must(cpv1beta2.AddToScheme(scheme))
	utilruntime.Must(infrastructurev1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr          string // deprecated, use capi's diagnostics-address instead
		enableLeaderElection bool
		secureMetrics        bool // deprecated, use capi's insecure-diagnostics instead
		enableHTTP2          bool // deprecated
		probeAddr            string
		enabledController    string
		concurrency          int
	)

	pflag.CommandLine.StringVar(&metricsAddr, "metrics-bind-address", ":8443", "[Deprecated, use --diagnostics-address instead] The address the metric endpoint binds to. "+
		"Use :8080 for http and :8443 for https. Setting to 0 will disable the metrics endpoint.")
	pflag.CommandLine.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	pflag.CommandLine.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	pflag.CommandLine.BoolVar(&secureMetrics, "metrics-secure", true,
		"[Deprecated, use --insecure-diagnostics instead] If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	pflag.CommandLine.BoolVar(&enableHTTP2, "enable-http2", false,
		"[Deprecated] If set, HTTP/2 will be enabled for the metrics and webhook servers")
	pflag.CommandLine.StringVar(&featureGates, "feature-gates", "", "feature gates to enable (comma separated list of key=value pairs)")
	pflag.CommandLine.IntVar(&concurrency, "concurrency", 5, "controller concurrency, default: 5")

	pflag.CommandLine.StringVar(&enabledController, "enable-controller", "", "The controller to enable. Default: all")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flags.AddManagerOptions(pflag.CommandLine, &managerOptions)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	err := featuregate.Configure(featureGates, os.Getenv(featuregate.EnvVarName))
	if err != nil {
		setupLog.Error(err, "unable to configure feature gates, the provided flags will be ignored")
	}

	if enabledController != "" && enabledController != allControllers {
		enabledControllers = map[string]bool{
			enabledController: true,
		}
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	var metricsOpts metricsserver.Options
	{
		tlsOpts, newMetricsOpts, err := flags.GetManagerOptions(managerOptions)
		if err != nil {
			setupLog.Error(err, "unable to start manager: invalid flags")
			os.Exit(1)
		}

		// this protocols list is not required starting golang.org/x/net@v0.17.0
		// see: https://github.com/advisories/GHSA-qppj-fm5r-hxr3
		// see: https://github.com/advisories/GHSA-4374-p667-p6c8
		disableHTTP2 := func(c *tls.Config) {
			setupLog.Info("disabling http/2")
			c.NextProtos = []string{"http/1.1"}
		}
		if !enableHTTP2 {
			tlsOpts = append(tlsOpts, disableHTTP2)
		}

		metricsOpts = *newMetricsOpts
		metricsOpts.TLSOpts = tlsOpts

		diagnosticsAddressSet := pflag.CommandLine.Changed("diagnostics-address") && pflag.Lookup("diagnostics-address").Value.String() != ":8443"
		insecureDiagnosticsSet := pflag.CommandLine.Changed("insecure-diagnostics")

		if !diagnosticsAddressSet {
			metricsOpts.BindAddress = metricsAddr
			setupLog.Info("Using legacy metrics configuration",
				"bindAddress", metricsAddr)
		}
		if !insecureDiagnosticsSet {
			metricsOpts.SecureServing = secureMetrics
			if secureMetrics {
				metricsOpts.FilterProvider = filters.WithAuthenticationAndAuthorization
			}
			setupLog.Info("Using legacy metrics configuration",
				"secureServing", secureMetrics)
		}
	}

	req, _ := labels.NewRequirement(clusterv1.ClusterNameLabel, selection.Exists, nil)
	clusterSecretCacheSelector := labels.NewSelector().Add(*req)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsOpts,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       fmt.Sprintf("%x.k0smotron.io", md5.Sum([]byte(enabledController))),
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Secret{}: {
					Label: clusterSecretCacheSelector,
				},
			},
		},
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					&corev1.ConfigMap{},
					&corev1.Secret{},
				},
				Unstructured: true,
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		setupLog.Error(err, "unable to get cluster config")
		os.Exit(1)
	}
	clientSet, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		setupLog.Error(err, "unable to get kubernetes clientset")
		os.Exit(1)
	}

	secretCachingClient, err := client.New(mgr.GetConfig(), client.Options{
		HTTPClient: mgr.GetHTTPClient(),
		Cache: &client.CacheOptions{
			Reader: mgr.GetCache(),
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to create secret caching client")
		os.Exit(1)
	}

	// Absent CAPI Machine kind means we are not running in a CAPI environment, so we don't need to run CAPI controllers.
	var runCAPIControllers bool
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		setupLog.Error(err, "unable to create discovery client")
		os.Exit(1)
	}

	resources, err := discoveryClient.ServerResourcesForGroupVersion(schema.GroupVersion{Group: "cluster.x-k8s.io", Version: "v1beta1"}.String())
	if err == nil && len(resources.APIResources) > 0 {
		runCAPIControllers = true
	} else {
		mgr.GetLogger().Info("Cluster API v1beta1 not installed, skipping cluster-api controllers setup")
	}

	//+kubebuilder:scaffold:builder
	ctrlOptions := capictrl.Options{
		MaxConcurrentReconciles: concurrency,
	}

	if isControllerEnabled(bootstrapController) && runCAPIControllers {
		if err = (&bootstrap.Controller{
			Client:              mgr.GetClient(),
			SecretCachingClient: secretCachingClient,
			Scheme:              mgr.GetScheme(),
			ClientSet:           clientSet,
			RESTConfig:          restConfig,
		}).SetupWithManager(mgr, ctrlOptions); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Bootstrap")
			os.Exit(1)
		}
		if err = (&bootstrap.ControlPlaneController{
			Client:              mgr.GetClient(),
			SecretCachingClient: secretCachingClient,
			Scheme:              mgr.GetScheme(),
			ClientSet:           clientSet,
			RESTConfig:          restConfig,
		}).SetupWithManager(mgr, ctrlOptions); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Bootstrap")
			os.Exit(1)
		}

		if err = (&bootstrapv1beta2.K0sWorkerConfigValidator{}).SetupK0sWorkerConfigWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create validation webhook", "webhook", "K0sWorkerConfigValidator")
			os.Exit(1)
		}
	}

	if isControllerEnabled(controlPlaneController) {
		// If 'control-plane' CAPI controller is explicitly enabled, it means also standalone controllers must be enabled
		setStandaloneControllers(mgr, clientSet, restConfig, ctrlOptions)

		if runCAPIControllers {
			if err = (&controlplane.K0smotronController{
				Client:              mgr.GetClient(),
				SecretCachingClient: secretCachingClient,
				Scheme:              mgr.GetScheme(),
				ClientSet:           clientSet,
				RESTConfig:          restConfig,
			}).SetupWithManager(mgr, ctrlOptions); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "K0smotronControlPlane")
				os.Exit(1)
			}

			if err = (&controlplane.K0sController{
				Client:              mgr.GetClient(),
				SecretCachingClient: secretCachingClient,
				ClientSet:           clientSet,
				RESTConfig:          restConfig,
			}).SetupWithManager(mgr, ctrlOptions); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "K0sController")
				os.Exit(1)
			}

			if err = (&cpv1beta2.K0sControlPlaneValidator{}).SetupK0sControlPlaneWebhookWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create validation webhook", "webhook", "K0sControlPlaneValidator")
				os.Exit(1)
			}

			if err = (&cpv1beta2.K0smotronControlPlaneValidator{}).SetupK0smotronControlPlaneWebhookWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create validation webhook", "webhook", "K0smotronControlPlaneValidator")
				os.Exit(1)
			}
		}
	} else if isControllerEnabled(standaloneController) {
		// If 'standalone' controller is explicitly enabled, run only standalone controllers.
		setStandaloneControllers(mgr, clientSet, restConfig, ctrlOptions)
	}

	if isControllerEnabled(infrastructureController) && runCAPIControllers {
		if err = (&infrastructure.RemoteMachineController{
			Client:              mgr.GetClient(),
			SecretCachingClient: secretCachingClient,
			Scheme:              mgr.GetScheme(),
			ClientSet:           clientSet,
			RESTConfig:          restConfig,
		}).SetupWithManager(mgr, ctrlOptions); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "RemoteMachine")
			os.Exit(1)
		}

		if err = (&infrastructure.ClusterController{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr, ctrlOptions); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "RemoteCluster")
			os.Exit(1)
		}
	}

	// ProviderID controller needs to run if either bootstrap or infrastructure controllers are running
	// as both of them create/update Machines.
	if runCAPIControllers && (isControllerEnabled(infrastructureController) || isControllerEnabled(bootstrapController)) {
		if err = (&bootstrap.ProviderIDController{
			Client:    mgr.GetClient(),
			Scheme:    mgr.GetScheme(),
			ClientSet: clientSet,
		}).SetupWithManager(mgr, ctrlOptions); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "ProviderID")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func isControllerEnabled(controllerName string) bool {
	return enabledControllers[controllerName]
}

func setStandaloneControllers(mgr manager.Manager, clientSet *kubernetes.Clientset, restConfig *rest.Config, opts capictrl.Options) {
	_ = mgr.AddReadyzCheck("webhook-check", mgr.GetWebhookServer().StartedChecker())
	if err := (&controller.ClusterReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		ClientSet:  clientSet,
		RESTConfig: restConfig,
		Recorder:   mgr.GetEventRecorderFor("cluster-reconciler"),
	}).SetupWithManager(mgr, opts); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "K0smotronCluster")
		os.Exit(1)
	}

	if err := controller.SetupK0sControlPlaneWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "k0smotron.Cluster")
		os.Exit(1)
	}

	if err := (&controller.JoinTokenRequestReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		ClientSet:  clientSet,
		RESTConfig: restConfig,
	}).SetupWithManager(mgr, opts); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "JoinTokenRequest")
		os.Exit(1)
	}
}
