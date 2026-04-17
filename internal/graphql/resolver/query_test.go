package resolver

import (
	"testing"

	"github.com/example/graphql-api/internal/datasource"
	"github.com/example/graphql-api/internal/graphql/generated"
	"github.com/example/graphql-api/internal/graphql/scalar"
)

func intPtr(v int) *int       { return &v }
func strPtr(v string) *string { return &v }

func TestBuildStarRocksQueryRequest_Basic(t *testing.T) {
	fields := []string{"id", "name"}
	filters := []*generated.StarRocksFilter{
		{Field: "status", Operator: generated.FilterOperatorEq, Value: "active"},
	}
	orderBy := []*generated.StarRocksOrderBy{
		{Field: "id", Direction: generated.SortDirectionDesc},
	}
	first := intPtr(10)

	req := buildStarRocksQueryRequest(fields, filters, orderBy, first, nil, nil, nil)

	if len(req.Fields) != 2 || req.Fields[0] != "id" || req.Fields[1] != "name" {
		t.Errorf("unexpected fields: %v", req.Fields)
	}
	if len(req.Filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(req.Filters))
	}
	if req.Filters[0].Field != "status" || req.Filters[0].Operator != datasource.FilterOpEQ {
		t.Errorf("unexpected filter: %+v", req.Filters[0])
	}
	if len(req.OrderBy) != 1 || req.OrderBy[0].Direction != datasource.SortDESC {
		t.Errorf("unexpected orderBy: %+v", req.OrderBy)
	}
	if req.Pagination == nil || req.Pagination.First == nil || *req.Pagination.First != 10 {
		t.Errorf("unexpected pagination: %+v", req.Pagination)
	}
}

func TestBuildStarRocksQueryRequest_NilFilters(t *testing.T) {
	req := buildStarRocksQueryRequest(nil, nil, nil, nil, nil, nil, nil)
	if len(req.Fields) != 0 {
		t.Errorf("expected no fields, got %v", req.Fields)
	}
	if len(req.Filters) != 0 {
		t.Errorf("expected no filters, got %v", req.Filters)
	}
	if req.Pagination != nil {
		t.Errorf("expected nil pagination, got %+v", req.Pagination)
	}
}

func TestBuildStarRocksQueryRequest_SkipsNilEntries(t *testing.T) {
	filters := []*generated.StarRocksFilter{
		nil,
		{Field: "x", Operator: generated.FilterOperatorGt, Value: "5"},
		nil,
	}
	orderBy := []*generated.StarRocksOrderBy{nil}

	req := buildStarRocksQueryRequest(nil, filters, orderBy, nil, nil, nil, nil)
	if len(req.Filters) != 1 {
		t.Errorf("expected 1 filter after skipping nils, got %d", len(req.Filters))
	}
	if len(req.OrderBy) != 0 {
		t.Errorf("expected 0 orderBy after skipping nils, got %d", len(req.OrderBy))
	}
}

func TestBuildPrometheusInstantRequest(t *testing.T) {
	filters := []*generated.PrometheusLabelFilter{
		{Name: "job", Value: "api", MatchType: generated.LabelMatchTypeExact},
	}
	req := buildPrometheusInstantRequest("up", nil, filters)

	if req.Options["query"] != "up" {
		t.Errorf("expected query=up, got %v", req.Options["query"])
	}
	if req.Options["query_type"] != "instant" {
		t.Errorf("expected query_type=instant, got %v", req.Options["query_type"])
	}
	if _, ok := req.Options["time"]; ok {
		t.Error("expected no time option when timeArg is nil")
	}
	if len(req.Filters) != 1 || req.Filters[0].Field != "job" {
		t.Errorf("unexpected filters: %+v", req.Filters)
	}
}

func TestBuildPrometheusRangeRequest(t *testing.T) {
	start := scalar.DateTime{}
	end := scalar.DateTime{}
	req := buildPrometheusRangeRequest("rate(http_requests_total[5m])", start, end, "15s", nil)

	if req.Options["query_type"] != "range" {
		t.Errorf("expected query_type=range, got %v", req.Options["query_type"])
	}
	if req.Options["step"] != "15s" {
		t.Errorf("expected step=15s, got %v", req.Options["step"])
	}
}

func TestConvertFilterOperator_AllValues(t *testing.T) {
	tests := []struct {
		in  generated.FilterOperator
		out datasource.FilterOperator
	}{
		{generated.FilterOperatorEq, datasource.FilterOpEQ},
		{generated.FilterOperatorNeq, datasource.FilterOpNEQ},
		{generated.FilterOperatorGt, datasource.FilterOpGT},
		{generated.FilterOperatorGte, datasource.FilterOpGTE},
		{generated.FilterOperatorLt, datasource.FilterOpLT},
		{generated.FilterOperatorLte, datasource.FilterOpLTE},
		{generated.FilterOperatorLike, datasource.FilterOpLIKE},
		{generated.FilterOperatorIn, datasource.FilterOpIN},
		{generated.FilterOperatorNotIn, datasource.FilterOpNOT_IN},
		{generated.FilterOperatorIsNull, datasource.FilterOpIS_NULL},
		{generated.FilterOperatorIsNotNull, datasource.FilterOpIS_NOT_NULL},
	}
	for _, tt := range tests {
		got := convertFilterOperator(tt.in)
		if got != tt.out {
			t.Errorf("convertFilterOperator(%v) = %v, want %v", tt.in, got, tt.out)
		}
	}
}

func TestConvertSortDirection(t *testing.T) {
	if convertSortDirection(generated.SortDirectionAsc) != datasource.SortASC {
		t.Error("expected ASC")
	}
	if convertSortDirection(generated.SortDirectionDesc) != datasource.SortDESC {
		t.Error("expected DESC")
	}
}

func TestConvertLabelMatchType(t *testing.T) {
	if convertLabelMatchType(generated.LabelMatchTypeExact) != datasource.FilterOpEQ {
		t.Error("expected EQ for EXACT")
	}
	if convertLabelMatchType(generated.LabelMatchTypeNotEqual) != datasource.FilterOpNEQ {
		t.Error("expected NEQ for NOT_EQUAL")
	}
}

func TestBuildStarRocksConnection_Empty(t *testing.T) {
	conn := buildStarRocksConnection(nil, nil, nil, nil)
	if len(conn.Nodes) != 0 || len(conn.Edges) != 0 {
		t.Error("expected empty nodes and edges")
	}
	if conn.TotalCount != 0 {
		t.Errorf("expected totalCount=0, got %d", conn.TotalCount)
	}
	if conn.PageInfo.HasNextPage || conn.PageInfo.HasPreviousPage {
		t.Error("expected no next/previous page")
	}
}

func TestBuildStarRocksConnection_WithData(t *testing.T) {
	data := []map[string]interface{}{
		{"id": 1, "name": "a"},
		{"id": 2, "name": "b"},
	}
	tc := int64(100)
	first := intPtr(10)
	offset := intPtr(5)

	conn := buildStarRocksConnection(data, &tc, offset, first)

	if len(conn.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(conn.Nodes))
	}
	if len(conn.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(conn.Edges))
	}
	if conn.TotalCount != 100 {
		t.Errorf("expected totalCount=100, got %d", conn.TotalCount)
	}
	if !conn.PageInfo.HasPreviousPage {
		t.Error("expected hasPreviousPage=true when offset > 0")
	}
	if conn.PageInfo.HasNextPage {
		t.Error("expected hasNextPage=false when data < first")
	}
	if conn.PageInfo.StartCursor == nil || conn.PageInfo.EndCursor == nil {
		t.Error("expected non-nil cursors")
	}
}

func TestBuildStarRocksConnection_HasNextPage(t *testing.T) {
	data := make([]map[string]interface{}, 10)
	for i := range data {
		data[i] = map[string]interface{}{"id": i}
	}
	first := intPtr(10)

	conn := buildStarRocksConnection(data, nil, nil, first)
	if !conn.PageInfo.HasNextPage {
		t.Error("expected hasNextPage=true when len(data) >= first")
	}
}

func TestConvertToInstantResult_Empty(t *testing.T) {
	result := &datasource.QueryResult{Data: nil}
	ir := convertToInstantResult(result)
	if ir.ResultType != "vector" {
		t.Errorf("expected resultType=vector, got %s", ir.ResultType)
	}
	if len(ir.Vectors) != 0 {
		t.Errorf("expected 0 vectors, got %d", len(ir.Vectors))
	}
}

func TestConvertToInstantResult_WithData(t *testing.T) {
	result := &datasource.QueryResult{
		Data: []map[string]interface{}{
			{
				"resultType": "vector",
				"metric":     map[string]interface{}{"job": "api"},
				"timestamp":  1234567890.0,
				"value":      42.5,
			},
		},
	}
	ir := convertToInstantResult(result)
	if len(ir.Vectors) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(ir.Vectors))
	}
	if len(ir.Vectors[0].Metric) != 1 {
		t.Errorf("expected 1 metric label, got %d", len(ir.Vectors[0].Metric))
	}
	if ir.Vectors[0].Value == nil {
		t.Error("expected non-nil value")
	}
}

func TestConvertToRangeResult_WithData(t *testing.T) {
	result := &datasource.QueryResult{
		Data: []map[string]interface{}{
			{
				"resultType": "matrix",
				"metric":     map[string]interface{}{"instance": "localhost:9090"},
				"values": []interface{}{
					map[string]interface{}{"timestamp": 1000.0, "value": 1.0},
					map[string]interface{}{"timestamp": 1015.0, "value": 2.0},
				},
			},
		},
	}
	rr := convertToRangeResult(result)
	if len(rr.Matrices) != 1 {
		t.Fatalf("expected 1 matrix, got %d", len(rr.Matrices))
	}
	if len(rr.Matrices[0].Values) != 2 {
		t.Errorf("expected 2 data points, got %d", len(rr.Matrices[0].Values))
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		in  interface{}
		out float64
	}{
		{42.5, 42.5},
		{float32(3.14), float64(float32(3.14))},
		{int(10), 10.0},
		{int64(100), 100.0},
		{"3.14", 3.14},
		{nil, 0},
		{true, 0},
	}
	for _, tt := range tests {
		got := toFloat64(tt.in)
		if got != tt.out {
			t.Errorf("toFloat64(%v) = %v, want %v", tt.in, got, tt.out)
		}
	}
}

func TestEncodeCursor(t *testing.T) {
	c := encodeCursor(0)
	if c == "" {
		t.Error("expected non-empty cursor")
	}
	c2 := encodeCursor(1)
	if c == c2 {
		t.Error("expected different cursors for different indices")
	}
}

// Ensure unused imports are referenced.
var _ = strPtr
