package controller

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	reconcileDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "agscale_reconcile_duration_seconds",
			Help:    "Duration of reconciliations by resource and result.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"resource", "result"},
	)

	reconcileErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agscale_reconcile_errors_total",
			Help: "Total reconcile errors by resource and reason.",
		},
		[]string{"resource", "reason"},
	)

	agentRemediationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agscale_agent_remediation_total",
			Help: "Total remediation actions applied.",
		},
		[]string{"action", "outcome"},
	)

	agentConditionTransitionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agscale_agent_condition_transitions_total",
			Help: "Total condition transitions for agents.",
		},
		[]string{"condition", "status"},
	)

	skillResolutionTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agscale_skill_resolution_total",
			Help: "Total skill source resolution attempts by protocol/outcome.",
		},
		[]string{"protocol", "outcome"},
	)

	skillResolutionDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "agscale_skill_resolution_duration_seconds",
			Help:    "Duration of skill source resolution by protocol.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"protocol"},
	)

	budgetEvaluationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agscale_budget_evaluations_total",
			Help: "Total budget policy evaluations by result and scope.",
		},
		[]string{"result", "scope"},
	)

	budgetExceededTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agscale_budget_exceeded_total",
			Help: "Total number of exceeded budget evaluations.",
		},
		[]string{"scope", "dimension"},
	)

	budgetTelemetryStaleTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agscale_budget_telemetry_stale_total",
			Help: "Total stale/unknown telemetry observations during budget evaluation.",
		},
		[]string{"source"},
	)

	dependencyCircuitTransitionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agscale_dependency_circuit_transitions_total",
			Help: "Total dependency circuit state transitions by dependency and event.",
		},
		[]string{"dependency", "event"},
	)

	llmFailoverTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agscale_llm_failover_total",
			Help: "Total backup LLM failover attempts by outcome.",
		},
		[]string{"outcome"},
	)

	workflowRunsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agscale_workflow_runs_total",
			Help: "Total workflow runs by result.",
		},
		[]string{"result"},
	)

	eventSourceTriggersTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agscale_eventsource_triggers_total",
			Help: "Total workflow run triggers emitted by event sources.",
		},
		[]string{"source"},
	)

	integrationHealthChecksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agscale_integration_health_checks_total",
			Help: "Total integration endpoint health checks by endpoint and outcome.",
		},
		[]string{"endpoint", "outcome"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		reconcileDurationSeconds,
		reconcileErrorsTotal,
		agentRemediationTotal,
		agentConditionTransitionsTotal,
		skillResolutionTotal,
		skillResolutionDurationSeconds,
		budgetEvaluationsTotal,
		budgetExceededTotal,
		budgetTelemetryStaleTotal,
		dependencyCircuitTransitionsTotal,
		llmFailoverTotal,
		workflowRunsTotal,
		eventSourceTriggersTotal,
		integrationHealthChecksTotal,
	)
}

func observeReconcile(resource, result string, started time.Time) {
	reconcileDurationSeconds.WithLabelValues(resource, result).Observe(time.Since(started).Seconds())
}
