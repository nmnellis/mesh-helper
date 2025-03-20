package prom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/util/stats"
	"github.com/prometheus/prometheus/util/teststorage"
	"time"
)

type PrometheusGetter struct {
	fakeAPI v1.API
}

type FakeAPI struct {
	v1.API  // Bogus embedded API. Will crash if any methods we don't override are called
	Storage *teststorage.TestStorage
	Engine  *promql.Engine
}

var _ v1.API = &FakeAPI{}

func (f FakeAPI) Query(ctx context.Context, query string, ts time.Time, opts ...v1.Option) (
	model.Value,
	v1.Warnings,
	error,
) {
	// Create a query
	qry, err := f.Engine.NewInstantQuery(context.Background(), f.Storage, nil, query, ts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create query: %w", err)
	}

	// Execute the query
	result := qry.Exec(context.Background())
	if result.Err != nil {
		return nil, nil, result.Err
	}

	// Print results
	//fmt.Printf("Query: %s\n", query)
	//fmt.Printf("Result: %v\n", result.Value)

	b, err := json.Marshal(QueryData{
		ResultType: result.Value.Type(),
		Result:     result.Value,
		Stats:      nil,
	})
	if err != nil {
		return nil, nil, err
	}
	var qres queryResult
	return qres.v, nil, json.Unmarshal(b, &qres)
}

func (f FakeAPI) QueryRange(ctx context.Context, query string, r v1.Range, opts ...v1.Option) (
	model.Value,
	v1.Warnings,
	error,
) {

	// Create a query
	qry, err := f.Engine.NewRangeQuery(context.Background(), f.Storage, nil, query, r.Start, r.End, r.Step)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create query: %w", err)
	}

	// Execute the query
	result := qry.Exec(context.Background())
	if result.Err != nil {
		return nil, nil, result.Err
	}

	// Print results
	// fmt.Printf("RangeQuery: %s\n", query)
	// fmt.Printf("Result: %v\n", result.Value)
	b, err := json.Marshal(QueryData{
		ResultType: result.Value.Type(),
		Result:     result.Value,
		Stats:      nil,
	})
	if err != nil {
		return nil, nil, err
	}
	if bytes.Equal(b, []byte("null")) {
		return model.Matrix{}, nil, nil
	}
	var qres queryResult
	return qres.v, nil, json.Unmarshal(b, &qres)
}

// queryResult contains result data for a query.
type queryResult struct {
	Type   model.ValueType `json:"resultType"`
	Result interface{}     `json:"result"`

	// The decoded value.
	v model.Value
}

func (qr *queryResult) UnmarshalJSON(b []byte) error {
	v := struct {
		Type   model.ValueType `json:"resultType"`
		Result json.RawMessage `json:"result"`
	}{}

	err := json.Unmarshal(b, &v)
	if err != nil {
		return err
	}

	switch v.Type {
	case model.ValScalar:
		var sv model.Scalar
		err = json.Unmarshal(v.Result, &sv)
		qr.v = &sv

	case model.ValVector:
		var vv model.Vector
		err = json.Unmarshal(v.Result, &vv)
		qr.v = vv

	case model.ValMatrix:
		var mv model.Matrix
		err = json.Unmarshal(v.Result, &mv)
		qr.v = mv

	default:
		err = fmt.Errorf("unexpected value type %q", v.Type)
	}
	return err
}

type QueryData struct {
	ResultType parser.ValueType `json:"resultType"`
	Result     parser.Value     `json:"result"`
	Stats      stats.QueryStats `json:"stats,omitempty"`
}
