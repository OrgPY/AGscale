package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	agscalev1alpha1 "agscale/api/v1alpha1"
	"agscale/internal/controller"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(agscalev1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool
	var webhookEnabled bool
	var webhookPort int

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.BoolVar(&webhookEnabled, "webhook-enabled", true, "Enable admission webhooks.")
	flag.IntVar(&webhookPort, "webhook-port", 9443, "Webhook server port.")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	options := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "agscale-controller-leader-election",
	}
	if webhookEnabled {
		options.WebhookServer = webhook.NewServer(webhook.Options{Port: webhookPort})
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		os.Exit(1)
	}

	if err := (&controller.AgentReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("agscale-controller"),
	}).SetupWithManager(mgr); err != nil {
		os.Exit(1)
	}
	if err := (&controller.AgentSkillReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("agscale-skill-controller"),
	}).SetupWithManager(mgr); err != nil {
		os.Exit(1)
	}
	if err := (&controller.AgentBudgetPolicyReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("agscale-budget-controller"),
	}).SetupWithManager(mgr); err != nil {
		os.Exit(1)
	}
	if err := (&controller.AgentWorkflowReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("agscale-workflow-controller"),
	}).SetupWithManager(mgr); err != nil {
		os.Exit(1)
	}
	if err := (&controller.AgentWorkflowRunReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("agscale-workflow-run-controller"),
	}).SetupWithManager(mgr); err != nil {
		os.Exit(1)
	}
	if err := (&controller.IntegrationEndpointReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("agscale-integration-controller"),
	}).SetupWithManager(mgr); err != nil {
		os.Exit(1)
	}
	if err := (&controller.EventSourceReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("agscale-eventsource-controller"),
	}).SetupWithManager(mgr); err != nil {
		os.Exit(1)
	}

	if webhookEnabled {
		agscalev1alpha1.SetWebhookReader(mgr.GetClient())
		if err := (&agscalev1alpha1.Agent{}).SetupWebhookWithManager(mgr); err != nil {
			os.Exit(1)
		}
		if err := (&agscalev1alpha1.AgentBudgetPolicy{}).SetupWebhookWithManager(mgr); err != nil {
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		os.Exit(1)
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		os.Exit(1)
	}
}
