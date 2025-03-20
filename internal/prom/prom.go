package prom

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/util/teststorage"
	"log"
	"os"
	"strconv"
	"time"
)

type PromtoolJson struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value"`
}

func LoadStorageFromFile(fname string) (*teststorage.TestStorage, error) {
	data, err := os.ReadFile(fname)
	if err != nil {
		return nil, err
	}
	var metrics []PromtoolJson
	if err := json.Unmarshal(data, &metrics); err != nil {
		return nil, err
	}

	storage, err := loadStorage(metrics)
	return storage, err
}

func LoadStorageFromEndpoint(server string, metric string) (*teststorage.TestStorage, error) {
	client, err := api.NewClient(api.Config{
		Address: fmt.Sprintf("%s", server),
	})
	if err != nil {
		return nil, fmt.Errorf("error creating client: %v", err)
	}

	v1api := v1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, warnings, err := v1api.Query(ctx, metric, time.Now())
	if err != nil {
		return nil, fmt.Errorf("error querying Prometheus: %v", err)
	}
	if len(warnings) > 0 {
		log.Printf("Warnings: %v", warnings)
	}
	storage, err := loadStorageHTTP(result.(model.Vector))
	return storage, err
}

func loadStorageHTTP(metrics model.Vector) (*teststorage.TestStorage, error) {
	// Create an in-memory storage
	var now = time.Now()

	storage, err := teststorage.NewWithError()
	if err != nil {
		return nil, err
	}
	// Insert your sample data
	appendable := storage.Appender(context.Background())
	fixedTs := now.UnixNano() / int64(time.Millisecond)
	fixedTsBefore := now.Add(-time.Minute).UnixNano() / int64(time.Millisecond)
	for _, m := range metrics {
		lb := labels.NewBuilder(labels.EmptyLabels())
		for k, v := range m.Metric {
			lb.Set(string(k), string(v))
		}
		lbls := lb.Labels()

		// We ignore the timestamp they gave us - just treat it as a constant
		v := float64(m.Value)
		ref, err := appendable.Append(0, lbls, fixedTsBefore, v-1)
		if err != nil {
			return nil, err
		}
		_, err = appendable.Append(ref, lbls, fixedTs, v)
		if err != nil {
			return nil, err
		}
	}

	if err := appendable.Commit(); err != nil {
		return nil, err
	}
	return storage, nil
}

func loadStorage(metrics []PromtoolJson) (*teststorage.TestStorage, error) {
	// Create an in-memory storage
	var now = time.Now()

	storage, err := teststorage.NewWithError()
	if err != nil {
		return nil, err
	}
	// Insert your sample data
	appendable := storage.Appender(context.Background())
	fixedTs := now.UnixNano() / int64(time.Millisecond)
	fixedTsBefore := now.Add(-time.Minute).UnixNano() / int64(time.Millisecond)
	for _, m := range metrics {
		lb := labels.NewBuilder(labels.EmptyLabels())
		for k, v := range m.Metric {
			lb.Set(k, v)
		}
		lbls := lb.Labels()

		// We ignore the timestamp they gave us - just treat it as a constant
		v, err := strconv.ParseFloat(m.Value[1].(string), 64)
		if err != nil {
			return nil, err
		}
		ref, err := appendable.Append(0, lbls, fixedTsBefore, v-1)
		if err != nil {
			return nil, err
		}
		_, err = appendable.Append(ref, lbls, fixedTs, v)
		if err != nil {
			return nil, err
		}
	}

	if err := appendable.Commit(); err != nil {
		return nil, err
	}
	return storage, nil
}
