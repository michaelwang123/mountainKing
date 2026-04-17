// Package resolver implements GraphQL query resolvers for the
// multi-datasource API. This file contains helper functions for query
// parameter conversion, result transformation, parallel multi-datasource
// dispatch, field selection optimization, and result set truncation.
package resolver

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/example/graphql-api/internal/datasource"
	"github.com/example/graphql-api/internal/graphql/generated"
	"github.com/example/graphql-api/internal/graphql/scalar"
)

// --- Data source name lookup ---

// findStarRocksDS returns the configured StarRocks data source name.
func findStarRocksDS(_ *queryResolver) string {
	return "analytics_db"
}

// findPrometheusDS returns the configured Prometheus data source name.
func findPrometheusDS(_ *queryResolver) string {
	return "monitoring"
}

// --- QueryRequest builders ---

// buildStarRocksQueryRequest converts GraphQL query parameters into a
// datasource.QueryRequest for the StarRocks adapter.
func buildStarRocksQueryRequest(
	fields []string,
	filters []*generated.StarRocksFilter,
	orderBy []*generated.StarRocksOrderBy,
	first *int, after *string, offset *int, limit *int,
) datasource.QueryRequest {
	req := datasource.QueryRequest{Fields: fields}

	for _, f := range filters {
		if f == nil {
			continue
		}
		req.Filters = append(req.Filters, datasource.FilterCondition{
			Field:    f.Field,
			Operator: convertFilterOperator(f.Operator),
			Value:    f.Value,
		})
	}

	for _, o := range orderBy {
		if o == nil {
			continue
		}
		req.OrderBy = append(req.OrderBy, datasource.OrderByClause{
			Field:     o.Field,
			Direction: convertSortDirection(o.Direction),
		})
	}

	if first != nil || after != nil || offset != nil || limit != nil {
		req.Pagination = &datasource.PaginationParams{
			First: first, After: after, Offset: offset, Limit: limit,
		}
	}

	return req
}

// buildPrometheusInstantRequest converts GraphQL instant query parameters
// into a datasource.QueryRequest for the Prometheus adapter.
func buildPrometheusInstantRequest(
	query string,
	timeArg *scalar.DateTime,
	filters []*generated.PrometheusLabelFilter,
) datasource.QueryRequest {
	opts := map[string]interface{}{
		"query":      query,
		"query_type": "instant",
	}
	if timeArg != nil {
		opts["time"] = timeArg.Time.Format(time.RFC3339)
	}

	req := datasource.QueryRequest{Options: opts}
	for _, f := range filters {
		if f == nil {
			continue
		}
		req.Filters = append(req.Filters, datasource.FilterCondition{
			Field:    f.Name,
			Operator: convertLabelMatchType(f.MatchType),
			Value:    f.Value,
		})
	}
	return req
}

// buildPrometheusRangeRequest converts GraphQL range query parameters
// into a datasource.QueryRequest for the Prometheus adapter.
func buildPrometheusRangeRequest(
	query string,
	startTime, endTime scalar.DateTime,
	step string,
	filters []*generated.PrometheusLabelFilter,
) datasource.QueryRequest {
	opts := map[string]interface{}{
		"query":      query,
		"query_type": "range",
		"startTime":  startTime.Time.Format(time.RFC3339),
		"endTime":    endTime.Time.Format(time.RFC3339),
		"step":       step,
	}

	req := datasource.QueryRequest{Options: opts}
	for _, f := range filters {
		if f == nil {
			continue
		}
		req.Filters = append(req.Filters, datasource.FilterCondition{
			Field:    f.Name,
			Operator: convertLabelMatchType(f.MatchType),
			Value:    f.Value,
		})
	}
	return req
}

// --- Enum conversions ---

func convertFilterOperator(op generated.FilterOperator) datasource.FilterOperator {
	switch op {
	case generated.FilterOperatorEq:
		return datasource.FilterOpEQ
	case generated.FilterOperatorNeq:
		return datasource.FilterOpNEQ
	case generated.FilterOperatorGt:
		return datasource.FilterOpGT
	case generated.FilterOperatorGte:
		return datasource.FilterOpGTE
	case generated.FilterOperatorLt:
		return datasource.FilterOpLT
	case generated.FilterOperatorLte:
		return datasource.FilterOpLTE
	case generated.FilterOperatorLike:
		return datasource.FilterOpLIKE
	case generated.FilterOperatorIn:
		return datasource.FilterOpIN
	case generated.FilterOperatorNotIn:
		return datasource.FilterOpNOT_IN
	case generated.FilterOperatorIsNull:
		return datasource.FilterOpIS_NULL
	case generated.FilterOperatorIsNotNull:
		return datasource.FilterOpIS_NOT_NULL
	default:
		return datasource.FilterOpEQ
	}
}

func convertSortDirection(dir generated.SortDirection) datasource.SortDirection {
	if dir == generated.SortDirectionDesc {
		return datasource.SortDESC
	}
	return datasource.SortASC
}

func convertLabelMatchType(mt generated.LabelMatchType) datasource.FilterOperator {
	switch mt {
	case generated.LabelMatchTypeExact:
		return datasource.FilterOpEQ
	case generated.LabelMatchTypeNotEqual:
		return datasource.FilterOpNEQ
	case generated.LabelMatchTypeRegex:
		return datasource.FilterOpLIKE
	case generated.LabelMatchTypeNotRegex:
		return datasource.FilterOpNOT_IN
	default:
		return datasource.FilterOpEQ
	}
}

// --- Result conversion ---

// buildStarRocksConnection converts raw query result data into a
// StarRocksConnection with Relay-style pagination.
func buildStarRocksConnection(
	data []map[string]interface{},
	totalCount *int64,
	offset *int,
	first *int,
) *generated.StarRocksConnection {
	nodes := make([]*generated.StarRocksRow, 0, len(data))
	edges := make([]*generated.StarRocksEdge, 0, len(data))

	startIdx := 0
	if offset != nil {
		startIdx = *offset
	}

	for i, row := range data {
		cursor := encodeCursor(startIdx + i)
		node := &generated.StarRocksRow{Data: scalar.JSON(row)}
		nodes = append(nodes, node)
		edges = append(edges, &generated.StarRocksEdge{
			Node: node, Cursor: cursor,
		})
	}

	var startCursor, endCursor *string
	if len(edges) > 0 {
		startCursor = &edges[0].Cursor
		endCursor = &edges[len(edges)-1].Cursor
	}

	hasNextPage := first != nil && len(data) >= *first

	tc := 0
	if totalCount != nil {
		tc = int(*totalCount)
	}

	return &generated.StarRocksConnection{
		Edges: edges,
		Nodes: nodes,
		PageInfo: &generated.PageInfo{
			HasNextPage:     hasNextPage,
			HasPreviousPage: startIdx > 0,
			StartCursor:     startCursor,
			EndCursor:       endCursor,
		},
		TotalCount: tc,
	}
}

// convertToInstantResult converts a datasource.QueryResult into a
// PrometheusInstantResult GraphQL type.
func convertToInstantResult(result *datasource.QueryResult) *generated.PrometheusInstantResult {
	vectors := make([]*generated.PrometheusVector, 0, len(result.Data))
	for _, row := range result.Data {
		vectors = append(vectors, &generated.PrometheusVector{
			Metric: extractMetricLabels(row),
			Value:  extractDataPoint(row),
		})
	}

	resultType := "vector"
	if len(result.Data) > 0 {
		if rt, ok := result.Data[0]["resultType"].(string); ok {
			resultType = rt
		}
	}

	return &generated.PrometheusInstantResult{
		ResultType: resultType,
		Vectors:    vectors,
	}
}

// convertToRangeResult converts a datasource.QueryResult into a
// PrometheusRangeResult GraphQL type.
func convertToRangeResult(result *datasource.QueryResult) *generated.PrometheusRangeResult {
	matrices := make([]*generated.PrometheusMatrix, 0, len(result.Data))
	for _, row := range result.Data {
		matrices = append(matrices, &generated.PrometheusMatrix{
			Metric: extractMetricLabels(row),
			Values: extractDataPoints(row),
		})
	}

	resultType := "matrix"
	if len(result.Data) > 0 {
		if rt, ok := result.Data[0]["resultType"].(string); ok {
			resultType = rt
		}
	}

	return &generated.PrometheusRangeResult{
		ResultType: resultType,
		Matrices:   matrices,
	}
}

// --- Prometheus data extraction helpers ---

func extractMetricLabels(row map[string]interface{}) []*generated.PrometheusMetricLabel {
	var labels []*generated.PrometheusMetricLabel
	metricRaw, ok := row["metric"]
	if !ok {
		return labels
	}
	metricMap, ok := metricRaw.(map[string]interface{})
	if !ok {
		return labels
	}
	for k, v := range metricMap {
		labels = append(labels, &generated.PrometheusMetricLabel{
			Name: k, Value: fmt.Sprintf("%v", v),
		})
	}
	return labels
}

func extractDataPoint(row map[string]interface{}) *generated.PrometheusDataPoint {
	dp := &generated.PrometheusDataPoint{}
	if ts, ok := row["timestamp"]; ok {
		dp.Timestamp = toFloat64(ts)
	}
	if val, ok := row["value"]; ok {
		f := toFloat64(val)
		dp.Value = &f
	}
	return dp
}

func extractDataPoints(row map[string]interface{}) []*generated.PrometheusDataPoint {
	valuesRaw, ok := row["values"]
	if !ok {
		return nil
	}
	values, ok := valuesRaw.([]interface{})
	if !ok {
		return nil
	}
	points := make([]*generated.PrometheusDataPoint, 0, len(values))
	for _, v := range values {
		pair, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		dp := &generated.PrometheusDataPoint{}
		if ts, ok := pair["timestamp"]; ok {
			dp.Timestamp = toFloat64(ts)
		}
		if val, ok := pair["value"]; ok {
			f := toFloat64(val)
			dp.Value = &f
		}
		points = append(points, dp)
	}
	return points
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return 0
	}
}

// --- Cursor encoding ---

func encodeCursor(index int) string {
	return base64.StdEncoding.EncodeToString(
		[]byte(fmt.Sprintf("cursor:%d", index)),
	)
}

// --- Field selection ---

// fieldRequested checks if a specific field name is present in the current
// GraphQL selection set using gqlgen's CollectAllFields.
func fieldRequested(ctx context.Context, fieldName string) bool {
	for _, f := range graphql.CollectAllFields(ctx) {
		if f == fieldName {
			return true
		}
	}
	return false
}

// --- Extensions warnings ---

// setExtensionWarnings registers warning messages in the GraphQL response
// extensions.warnings field. It safely handles the case where warnings
// have already been registered by merging them.
func setExtensionWarnings(ctx context.Context, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	existing, _ := graphql.GetExtension(ctx, "warnings").([]string)
	merged := append(existing, warnings...)
	rctx := graphql.GetOperationContext(ctx)
	if rctx == nil {
		return
	}
	rctx.Extensions["warnings"] = merged
}

// --- Parallel multi-datasource query support ---

// queryResult holds the result of a single data source query for
// parallel dispatch.
type queryResult struct {
	dsName string
	result *datasource.QueryResult
	err    error
}

// executeParallel executes multiple data source queries in parallel using
// sync.WaitGroup with independent result collection. Each query runs in
// its own goroutine and failures do not cancel other queries (per
// Requirement 6.3). This uses sync.WaitGroup + independent result
// collection, NOT errgroup.WithContext.
func executeParallel(
	ctx context.Context,
	mgr *datasource.DataSourceManager,
	queries map[string]datasource.QueryRequest,
) []queryResult {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []queryResult
	)

	for dsName, req := range queries {
		wg.Add(1)
		go func(name string, r datasource.QueryRequest) {
			defer wg.Done()
			res, err := mgr.ExecuteWithRetry(ctx, name, r)
			mu.Lock()
			results = append(results, queryResult{
				dsName: name, result: res, err: err,
			})
			mu.Unlock()
		}(dsName, req)
	}

	wg.Wait()
	return results
}
