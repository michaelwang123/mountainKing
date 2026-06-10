package resolver

import (
	"context"

	"github.com/99designs/gqlgen/graphql"
	"github.com/michaelwang123/mountainKing/internal/graphql/generated"
	"github.com/michaelwang123/mountainKing/internal/graphql/scalar"
	"github.com/michaelwang123/mountainKing/internal/template"
)

func convertJSONToMap(j scalar.JSON) map[string]any {
	if j == nil {
		return nil
	}
	result := make(map[string]any, len(j))
	for k, v := range j {
		result[k] = v
	}
	return result
}

func convertOrderBy(orderBy []*generated.TemplateOrderBy) []template.TemplateOrderByParam {
	if len(orderBy) == 0 {
		return nil
	}
	result := make([]template.TemplateOrderByParam, 0, len(orderBy))
	for _, o := range orderBy {
		if o == nil {
			continue
		}
		result = append(result, template.TemplateOrderByParam{
			Field:     o.Field,
			Direction: o.Direction.String(),
		})
	}
	return result
}

func skipCacheRequested(ctx context.Context) bool {
	oc := graphql.GetOperationContext(ctx)
	if oc == nil {
		return false
	}
	cacheVal, ok := oc.Extensions["cache"]
	if !ok {
		return false
	}
	b, ok := cacheVal.(bool)
	return ok && !b
}

func buildTemplateQueryConnection(
	data []map[string]any,
	originalLen int,
	totalCount *int64,
	offset *int,
	first *int,
) *generated.TemplateQueryConnection {
	nodes := make([]scalar.JSON, 0, len(data))
	for _, row := range data {
		nodes = append(nodes, scalar.JSON(row))
	}

	hasNextPage := first != nil && originalLen > *first
	hasPreviousPage := offset != nil && *offset > 0

	tc := 0
	if totalCount != nil {
		tc = int(*totalCount)
	}

	return &generated.TemplateQueryConnection{
		Nodes: nodes,
		PageInfo: &generated.PageInfo{
			HasNextPage:     hasNextPage,
			HasPreviousPage: hasPreviousPage,
		},
		TotalCount: tc,
	}
}

func convertReloadResult(r *template.ReloadResult) *generated.ReloadTemplatesResult {
	failures := make([]*generated.TemplateLoadFailure, 0, len(r.Failures))
	for _, f := range r.Failures {
		failures = append(failures, &generated.TemplateLoadFailure{
			Name:  f.Name,
			Error: f.Error,
		})
	}
	return &generated.ReloadTemplatesResult{
		SuccessCount: r.SuccessCount,
		Failures:     failures,
		Duration:     r.Duration.String(),
	}
}
