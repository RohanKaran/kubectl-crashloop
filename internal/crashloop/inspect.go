package crashloop

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type logFetcher func(context.Context, string, string, string, int64, bool) (string, error)

// InspectorOption customizes an Inspector during construction.
type InspectorOption func(*Inspector)

// Inspector assembles crash reports from pod state, warning Events, and logs.
type Inspector struct {
	client     kubernetes.Interface
	logFetcher logFetcher
	now        func() time.Time
}

type containerStatusRef struct {
	Name   string
	Status corev1.ContainerStatus
}

type podLogRequest struct {
	namespace string
	podName   string
	tailLines int64
}

// NewInspector constructs an Inspector backed by the provided Kubernetes client.
func NewInspector(client kubernetes.Interface, opts ...InspectorOption) *Inspector {
	inspector := &Inspector{
		client:     client,
		logFetcher: defaultLogFetcher(client),
		now:        time.Now,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(inspector)
		}
	}

	return inspector
}

// WithLogFetcher overrides log retrieval, primarily for tests.
func WithLogFetcher(fetcher logFetcher) InspectorOption {
	return func(i *Inspector) {
		if fetcher != nil {
			i.logFetcher = fetcher
		}
	}
}

// WithNowFunc overrides the clock used when stamping generated reports.
func WithNowFunc(now func() time.Time) InspectorOption {
	return func(i *Inspector) {
		if now != nil {
			i.now = now
		}
	}
}

// Inspect builds a crash report for the requested pod and optional container.
func (i *Inspector) Inspect(ctx context.Context, req Request) (*CrashReport, error) {
	report := &CrashReport{
		PodName:     req.PodName,
		Namespace:   req.Namespace,
		ContextName: req.ContextName,
		GeneratedAt: i.now().UTC(),
	}

	pod, err := i.client.CoreV1().Pods(req.Namespace).Get(ctx, req.PodName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, nsErr := i.client.CoreV1().Namespaces().Get(ctx, req.Namespace, metav1.GetOptions{}); nsErr != nil && apierrors.IsNotFound(nsErr) {
				return nil, nsErr
			}
		}
		return nil, err
	}

	logRequest := podLogRequest{
		namespace: req.Namespace,
		podName:   req.PodName,
		tailLines: req.TailLines,
	}

	statuses := collectStatuses(pod)
	selectedStatuses, err := filterStatuses(statuses, req.Container)
	if err != nil {
		return nil, err
	}

	baselineEntries, baselineWarnings := i.buildBaselineEntries(ctx, logRequest, selectedStatuses)
	report.Warnings = append(report.Warnings, baselineWarnings...)

	eventEntries, eventWarnings, err := i.buildEventEntries(ctx, pod, statuses, req.Container)
	report.Warnings = append(report.Warnings, eventWarnings...)
	if err != nil {
		switch {
		case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
			report.Warnings = append(report.Warnings, fmt.Sprintf("Unable to list warning Events for %s/%s: %v", req.Namespace, req.PodName, err))
		default:
			return nil, err
		}
	} else {
		eventEntries, eventLogWarnings := i.attachCurrentLogsToEventEntries(ctx, logRequest, selectedStatuses, baselineEntries, eventEntries)
		report.Warnings = append(report.Warnings, eventLogWarnings...)
		if len(eventEntries) == 0 && len(baselineEntries) > 0 {
			report.Warnings = append(report.Warnings, "Historical Events may have expired on this cluster; showing baseline pod termination state.")
		}
	}

	entries := mergeEntries(append(eventEntries, baselineEntries...))
	if req.Limit > 0 && len(entries) > req.Limit {
		entries = entries[:req.Limit]
	}

	report.Warnings = uniqueStrings(report.Warnings)
	report.Entries = entries
	return report, nil
}
