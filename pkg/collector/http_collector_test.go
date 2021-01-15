package collector

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/api/autoscaling/v2beta2"
	autoscaling "k8s.io/api/autoscaling/v2beta2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/resource"
)

type testExternalMetricsHandler struct {
	values []int64
	test   *testing.T
}

func (t testExternalMetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	response, err := json.Marshal(testMetricResponse{t.values})
	require.NoError(t.test, err)
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(response)
	require.NoError(t.test, err)
}

func makeHTTPTestServer(t *testing.T, values []int64) string {
	server := httptest.NewServer(&testExternalMetricsHandler{values: values, test: t})
	return server.URL
}

func parseQuantiry(input string) resource.Quantity {
	quantity, _ := resource.ParseQuantity(input)
	return quantity
}

func TestHTTPCollector(t *testing.T) {
	for _, tc := range []struct {
		name       string
		values     []int64
		output     int
		aggregator string
		quantity   resource.Quantity
	}{
		{
			name:       "basic",
			values:     []int64{3},
			output:     3,
			aggregator: "sum",
			quantity:   parseQuantiry("3"),
		},
		{
			name:       "sum",
			values:     []int64{3, 5, 6},
			aggregator: "sum",
			output:     14,
			quantity:   parseQuantiry("14"),
		},
		{
			name:       "average",
			values:     []int64{3, 5, 6},
			aggregator: "sum",
			output:     14,
			quantity:   parseQuantiry("14"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testServer := makeHTTPTestServer(t, tc.values)
			plugin, err := NewHTTPCollectorPlugin()
			require.NoError(t, err)
			testConfig := makeTestHTTPCollectorConfig(testServer, tc.aggregator)

			hpa := &autoscaling.HorizontalPodAutoscaler{
				ObjectMeta: v1.ObjectMeta{
					Name:      "hpa1",
					Namespace: "default",
					Annotations: map[string]string{
						"metric-config.pods.requests-per-second.json-path/json-key": "$.http_server.rps",
						"metric-config.pods.requests-per-second.json-path/path":     "/metrics",
						"metric-config.pods.requests-per-second.json-path/port":     "9090",
					},
				},
				Spec: autoscaling.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscaling.CrossVersionObjectReference{
						Kind:       "Deployment",
						Name:       "app",
						APIVersion: "apps/v1",
					},
					MinReplicas: &[]int32{1}[0],
					MaxReplicas: 10,
					Metrics: []autoscaling.MetricSpec{
						{
							Type: autoscaling.PodsMetricSourceType,
							Pods: &autoscaling.PodsMetricSource{
								Metric: autoscaling.MetricIdentifier{
									Name: "requests-per-second",
								},
								Target: autoscaling.MetricTarget{
									Type:         autoscaling.AverageValueMetricType,
									AverageValue: &tc.quantity,
								},
							},
						},
					},
				},
			}

			collector, err := plugin.NewCollector(hpa, testConfig, testInterval)
			require.NoError(t, err)
			metrics, err := collector.GetMetrics()
			require.NoError(t, err)
			require.NotNil(t, metrics)
			require.Len(t, metrics, 1)
			require.EqualValues(t, metrics[0].External.Value.Value(), tc.output)
		})
	}
}

func makeTestHTTPCollectorConfig(endpoint, aggregator string) *MetricConfig {
	config := &MetricConfig{
		MetricTypeName: MetricTypeName{
			Type: v2beta2.ExternalMetricSourceType,
			Metric: v2beta2.MetricIdentifier{
				Name: "test-metric",
				Selector: &v1.LabelSelector{
					MatchLabels: map[string]string{identifierLabel: "test-metric"},
				},
			},
		},
		Config: map[string]string{
			HTTPJsonPathAnnotationKey: "$.values",
			HTTPEndpointAnnotationKey: endpoint,
		},
	}
	if aggregator != "" {
		config.Config["aggregator"] = aggregator
	}
	return config
}
