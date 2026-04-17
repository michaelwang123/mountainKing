package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/example/graphql-api/internal/datasource"
	apierrors "github.com/example/graphql-api/internal/errors"
)

// InstrumentedTransport wraps an http.RoundTripper to track active connections
// via an atomic counter. This enables Prometheus connection pool metrics
// (graphql_datasource_connection_pool_active) for the HTTP-based Prometheus adapter.
type InstrumentedTransport struct {
	base        http.RoundTripper
	activeConns atomic.Int64
}

// RoundTrip executes the HTTP request while tracking active connections.
// The counter is incremented before the request and decremented when the
// response body is closed.
func (t *InstrumentedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.activeConns.Add(1)
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		t.activeConns.Add(-1)
		return nil, err
	}
	resp.Body = &instrumentedBody{ReadCloser: resp.Body, transport: t}
	return resp, nil
}

// ActiveConns returns the current number of active (in-flight) connections.
func (t *InstrumentedTransport) ActiveConns() int64 {
	return t.activeConns.Load()
}

// instrumentedBody wraps an io.ReadCloser to decrement the active connection
// counter exactly once when the body is closed.
type instrumentedBody struct {
	io.ReadCloser
	transport *InstrumentedTransport
	once      sync.Once
}

// Close closes the underlying body and decrements the active connection counter.
func (b *instrumentedBody) Close() error {
	b.once.Do(func() { b.transport.activeConns.Add(-1) })
	return b.ReadCloser.Close()
}

// defaultMaxDataPoints is the default limit for data points returned by a
// Prometheus query before an error is raised.
const defaultMaxDataPoints = 11000

// Adapter implements datasource.DataSource for Prometheus.
// It communicates with Prometheus via its HTTP API using a custom
// InstrumentedTransport for connection pool metrics tracking.
type Adapter struct {
	name          string
	config        datasource.DataSourceConfig
	baseURL       string
	client        *http.Client
	transport     *InstrumentedTransport
	queryBuilder  *PromQLQueryBuilder
	logger        *zap.Logger
	mu            sync.RWMutex
	available     bool
	maxDataPoints int
}

// Verify interface compliance at compile time.
var _ datasource.DataSource = (*Adapter)(nil)

// NewAdapter creates a new Prometheus adapter from config.
// It extracts the base URL from config.Connection["url"] and
// max_data_points from config.Options.
func NewAdapter(name string, config datasource.DataSourceConfig, logger *zap.Logger) (*Adapter, error) {
	baseURL, _ := config.Connection["url"].(string)
	if baseURL == "" {
		return nil, fmt.Errorf("prometheus adapter %q: missing connection.url", name)
	}

	// Parse and validate the URL.
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("prometheus adapter %q: invalid connection.url: %w", name, err)
	}

	maxDP := getIntOpt(config.Options, "max_data_points", defaultMaxDataPoints)

	transport := &InstrumentedTransport{
		base: http.DefaultTransport,
	}

	queryTimeout := getDurationOpt(config.Options, "query_timeout", 15*time.Second)

	client := &http.Client{
		Transport: transport,
		Timeout:   queryTimeout,
	}

	return &Adapter{
		name:          name,
		config:        config,
		baseURL:       baseURL,
		client:        client,
		transport:     transport,
		queryBuilder:  NewPromQLQueryBuilder(),
		logger:        logger,
		maxDataPoints: maxDP,
	}, nil
}

// Factory returns an AdapterFactory for registering with the AdapterRegistry.
func Factory(logger *zap.Logger) datasource.AdapterFactory {
	return func(name string, config datasource.DataSourceConfig) (datasource.DataSource, error) {
		return NewAdapter(name, config, logger)
	}
}

// Name returns the data source name.
func (a *Adapter) Name() string {
	return a.name
}

// Type returns "prometheus".
func (a *Adapter) Type() string {
	return "prometheus"
}

// Connect verifies the Prometheus endpoint is reachable by sending
// GET /api/v1/status/buildinfo. Connect is idempotent.
func (a *Adapter) Connect(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.available {
		return nil
	}

	reqURL := a.baseURL + "/api/v1/status/buildinfo"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("prometheus adapter %q: create request: %w", a.name, err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("prometheus adapter %q: connect failed: %w", a.name, err)
	}
	defer resp.Body.Close()
	// Drain body to allow connection reuse.
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("prometheus adapter %q: buildinfo returned status %d", a.name, resp.StatusCode)
	}

	a.available = true
	a.logger.Info("prometheus adapter connected",
		zap.String("name", a.name),
		zap.String("url", a.baseURL),
	)
	return nil
}

// IsAvailable returns whether the adapter is currently connected.
func (a *Adapter) IsAvailable() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.available
}

// Execute runs a query against Prometheus. It determines instant vs range
// query from Options, builds the PromQL query, validates labels, calls the
// Prometheus HTTP API, parses the JSON response, converts special values
// (NaN/Inf), and checks max_data_points.
func (a *Adapter) Execute(ctx context.Context, req datasource.QueryRequest) (*datasource.QueryResult, error) {
	a.mu.RLock()
	avail := a.available
	a.mu.RUnlock()

	if !avail {
		return nil, apierrors.DatasourceError(
			apierrors.ErrDatasourceUnavailable,
			fmt.Sprintf("prometheus adapter %q is not connected", a.name),
		)
	}

	// Validate label values in filters to prevent PromQL injection.
	for _, f := range req.Filters {
		val := fmt.Sprintf("%v", f.Value)
		if err := ValidateLabelValue(val); err != nil {
			return nil, err
		}
	}

	// Validate the query expression if present.
	if q, ok := req.Options["query"].(string); ok && q != "" {
		if err := ValidateQueryExpression(q); err != nil {
			return nil, err
		}
	}

	// Determine instant vs range query.
	isRange := false
	if _, ok := req.Options["startTime"]; ok {
		isRange = true
	}

	var (
		apiPath string
		params  url.Values
		query   string
		err     error
	)

	if isRange {
		query, params, err = a.queryBuilder.BuildRange(req)
		if err != nil {
			return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
				fmt.Sprintf("prometheus build range query: %v", err))
		}
		apiPath = "/api/v1/query_range"
	} else {
		query, params, err = a.queryBuilder.BuildInstant(req)
		if err != nil {
			return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
				fmt.Sprintf("prometheus build instant query: %v", err))
		}
		apiPath = "/api/v1/query"
	}

	_ = query // query string is embedded in params

	// Execute the HTTP request.
	promResp, err := a.doQuery(ctx, apiPath, params)
	if err != nil {
		return nil, err
	}

	// Convert Prometheus response to QueryResult.
	return a.convertResponse(promResp)
}

// prometheusAPIResponse represents the top-level JSON response from the
// Prometheus HTTP API.
type prometheusAPIResponse struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	ErrorType string          `json:"errorType,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// prometheusData represents the data field in a Prometheus API response.
type prometheusData struct {
	ResultType PrometheusResultType `json:"resultType"`
	Result     json.RawMessage      `json:"result"`
}

// vectorResult represents a single instant vector sample.
type vectorResult struct {
	Metric map[string]string  `json:"metric"`
	Value  [2]json.RawMessage `json:"value"` // [timestamp, value_string]
}

// matrixResult represents a single range vector series.
type matrixResult struct {
	Metric map[string]string    `json:"metric"`
	Values [][2]json.RawMessage `json:"values"` // [[timestamp, value_string], ...]
}

// doQuery executes an HTTP GET request against the Prometheus API and parses
// the JSON response.
func (a *Adapter) doQuery(ctx context.Context, path string, params url.Values) (*prometheusData, error) {
	reqURL := a.baseURL + path + "?" + params.Encode()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("prometheus create request: %v", err))
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("prometheus query failed: %v", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("prometheus read response: %v", err))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("prometheus returned status %d: %s", resp.StatusCode, string(body)))
	}

	var apiResp prometheusAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("prometheus parse response: %v", err))
	}

	if apiResp.Status != "success" {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("prometheus error (%s): %s", apiResp.ErrorType, apiResp.Error))
	}

	var data prometheusData
	if err := json.Unmarshal(apiResp.Data, &data); err != nil {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("prometheus parse data: %v", err))
	}

	return &data, nil
}

// convertResponse converts a Prometheus API data response into a unified
// QueryResult. It handles vector and matrix result types, converts special
// float values (NaN/Inf), and enforces the max_data_points limit.
func (a *Adapter) convertResponse(data *prometheusData) (*datasource.QueryResult, error) {
	var warnings []string

	switch data.ResultType {
	case ResultTypeVector:
		return a.convertVector(data.Result)
	case ResultTypeMatrix:
		return a.convertMatrix(data.Result, &warnings)
	case ResultTypeScalar:
		return a.convertScalar(data.Result)
	case ResultTypeString:
		return a.convertString(data.Result)
	default:
		// Fallback: return raw data as a single row.
		row := map[string]interface{}{
			"resultType": string(data.ResultType),
			"raw":        string(data.Result),
		}
		return &datasource.QueryResult{Data: []map[string]interface{}{row}}, nil
	}
}

// convertVector converts a Prometheus instant vector result.
func (a *Adapter) convertVector(raw json.RawMessage) (*datasource.QueryResult, error) {
	var results []vectorResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("prometheus parse vector: %v", err))
	}

	// Check max data points.
	if len(results) > a.maxDataPoints {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceMaxDataPoints,
			fmt.Sprintf("query returned %d data points, exceeding limit of %d; consider narrowing the query",
				len(results), a.maxDataPoints))
	}

	var warnings []string
	data := make([]map[string]interface{}, 0, len(results))
	for _, r := range results {
		ts, val, w := parseTimestampValue(r.Value)
		warnings = append(warnings, w...)

		row := map[string]interface{}{
			"metric":    r.Metric,
			"value":     val,
			"timestamp": ts,
		}
		data = append(data, row)
	}

	return &datasource.QueryResult{Data: data, Warnings: warnings}, nil
}

// convertMatrix converts a Prometheus range vector (matrix) result.
func (a *Adapter) convertMatrix(raw json.RawMessage, _ *[]string) (*datasource.QueryResult, error) {
	var results []matrixResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("prometheus parse matrix: %v", err))
	}

	// Count total data points across all series.
	totalPoints := 0
	for _, r := range results {
		totalPoints += len(r.Values)
	}
	if totalPoints > a.maxDataPoints {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceMaxDataPoints,
			fmt.Sprintf("query returned %d data points, exceeding limit of %d; consider increasing step or narrowing time range",
				totalPoints, a.maxDataPoints))
	}

	var warnings []string
	data := make([]map[string]interface{}, 0, len(results))
	for _, r := range results {
		timestamps := make([]float64, 0, len(r.Values))
		values := make([]*float64, 0, len(r.Values))

		for i, pair := range r.Values {
			ts, val, w := parseTimestampValue(pair)
			for _, ww := range w {
				warnings = append(warnings, fmt.Sprintf("series %v data point [%d]: %s",
					r.Metric, i, ww))
			}
			timestamps = append(timestamps, ts)
			values = append(values, val)
		}

		row := map[string]interface{}{
			"metric":     r.Metric,
			"values":     values,
			"timestamps": timestamps,
		}
		data = append(data, row)
	}

	return &datasource.QueryResult{Data: data, Warnings: warnings}, nil
}

// convertScalar converts a Prometheus scalar result.
func (a *Adapter) convertScalar(raw json.RawMessage) (*datasource.QueryResult, error) {
	var pair [2]json.RawMessage
	if err := json.Unmarshal(raw, &pair); err != nil {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("prometheus parse scalar: %v", err))
	}

	ts, val, warnings := parseTimestampValue(pair)
	row := map[string]interface{}{
		"value":     val,
		"timestamp": ts,
	}
	return &datasource.QueryResult{Data: []map[string]interface{}{row}, Warnings: warnings}, nil
}

// convertString converts a Prometheus string result.
func (a *Adapter) convertString(raw json.RawMessage) (*datasource.QueryResult, error) {
	var pair [2]json.RawMessage
	if err := json.Unmarshal(raw, &pair); err != nil {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("prometheus parse string: %v", err))
	}

	var ts float64
	if err := json.Unmarshal(pair[0], &ts); err != nil {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("prometheus parse string timestamp: %v", err))
	}

	var val string
	if err := json.Unmarshal(pair[1], &val); err != nil {
		return nil, apierrors.DatasourceError(apierrors.ErrDatasourceQueryError,
			fmt.Sprintf("prometheus parse string value: %v", err))
	}

	row := map[string]interface{}{
		"value":     val,
		"timestamp": ts,
	}
	return &datasource.QueryResult{Data: []map[string]interface{}{row}}, nil
}

// parseTimestampValue parses a Prometheus [timestamp, value_string] pair.
// It handles NaN and ±Inf values using ConvertValue.
func parseTimestampValue(pair [2]json.RawMessage) (float64, *float64, []string) {
	var ts float64
	if err := json.Unmarshal(pair[0], &ts); err != nil {
		return 0, nil, []string{fmt.Sprintf("failed to parse timestamp: %v", err)}
	}

	var valStr string
	if err := json.Unmarshal(pair[1], &valStr); err != nil {
		return ts, nil, []string{fmt.Sprintf("failed to parse value: %v", err)}
	}

	f, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return ts, nil, []string{fmt.Sprintf("failed to parse float value %q: %v", valStr, err)}
	}

	converted, warning := ConvertValue(f)
	var warnings []string
	if warning != "" {
		warnings = append(warnings, warning)
	}
	return ts, converted, warnings
}

// HealthCheck verifies the Prometheus connection by sending GET /api/v1/status/buildinfo
// with a timeout of query_timeout/3.
func (a *Adapter) HealthCheck(ctx context.Context) error {
	queryTimeout := getDurationOpt(a.config.Options, "query_timeout", 15*time.Second)
	healthTimeout := queryTimeout / 3
	if healthTimeout < time.Second {
		healthTimeout = time.Second
	}

	hctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()

	reqURL := a.baseURL + "/api/v1/status/buildinfo"
	req, err := http.NewRequestWithContext(hctx, http.MethodGet, reqURL, nil)
	if err != nil {
		a.setAvailable(false)
		return fmt.Errorf("prometheus adapter %q: health check create request: %w", a.name, err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		a.setAvailable(false)
		return fmt.Errorf("prometheus adapter %q: health check failed: %w", a.name, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		a.setAvailable(false)
		return fmt.Errorf("prometheus adapter %q: health check returned status %d", a.name, resp.StatusCode)
	}

	a.setAvailable(true)
	return nil
}

// SchemaFiles returns the Prometheus GraphQL schema file paths.
func (a *Adapter) SchemaFiles() []string {
	return []string{"internal/graphql/schema/prometheus.graphql"}
}

// Close marks the adapter as unavailable and closes idle connections.
func (a *Adapter) Close(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.available = false
	a.client.CloseIdleConnections()
	return nil
}

// setAvailable updates the availability status under lock.
func (a *Adapter) setAvailable(v bool) {
	a.mu.Lock()
	a.available = v
	a.mu.Unlock()
}

// getIntOpt extracts an integer option from the options map with a default fallback.
func getIntOpt(options map[string]interface{}, key string, defaultVal int) int {
	if options == nil {
		return defaultVal
	}
	v, ok := options[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case string:
		if parsed, err := strconv.Atoi(val); err == nil {
			return parsed
		}
	}
	return defaultVal
}

// getDurationOpt extracts a duration option from the options map with a default fallback.
func getDurationOpt(options map[string]interface{}, key string, defaultVal time.Duration) time.Duration {
	if options == nil {
		return defaultVal
	}
	v, ok := options[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case string:
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	case time.Duration:
		return val
	case float64:
		return time.Duration(val) * time.Second
	case int:
		return time.Duration(val) * time.Second
	}
	return defaultVal
}
