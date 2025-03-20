package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/nmnellis/mesh-helper/internal/domain"
	"github.com/nmnellis/mesh-helper/internal/prom"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/util/teststorage"
	"github.com/spf13/cobra"
	"istio.io/api/meta/v1alpha1"
	v1 "istio.io/api/security/v1"
	securityv1beta1 "istio.io/api/security/v1beta1"
	v1beta1api "istio.io/api/type/v1beta1"
	"istio.io/client-go/pkg/apis/security/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"strings"
	"time"
)

var (
	Name    string
	File    string
	Output  string
	PromURL string
	Audit   bool
	Metric  string
)

func dependenciesCmd(ctx context.Context, globalFlags *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "dependencies",
		Aliases: []string{"dep"},
		Short:   "List application dependencies",
		Long:    ` `,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(globalFlags)
		},
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		SilenceUsage: true,
	}
	// set global CLI flags
	globalFlags.AddToFlags(cmd.PersistentFlags())
	cmd.Flags().StringVarP(&Output, "output", "o", "tree", "Output Format (tree, authz)")
	cmd.Flags().StringVarP(&File, "file", "f", "", "Read from a prometheus formatted input file")
	cmd.Flags().StringVar(&Name, "name", "", "Filter for workload by name")
	cmd.Flags().StringVar(&PromURL, "prom-url", "", "Call prometheus directly to fetch data")
	cmd.Flags().BoolVar(&Audit, "audit", true, "Audit traffic rather than deny")
	cmd.Flags().StringVar(&Metric, "metric", "istio_tcp_sent_bytes_total", "Metric to grab dependency tree (istio_tcp_sent_bytes_total, istio_requests_total)")

	return cmd
}

type YamlMarshaller struct{}

func (YamlMarshaller) ToYaml(resource interface{}) ([]byte, error) {
	switch typedResource := resource.(type) {
	case nil:
		return []byte{}, nil
	case proto.Message:
		buf := &bytes.Buffer{}
		if err := (&jsonpb.Marshaler{OrigName: true}).Marshal(buf, typedResource); err != nil {
			return nil, err
		}
		return yaml.JSONToYAML(buf.Bytes())
	default:
		return yaml.Marshal(resource)
	}
}

func run(globalFlags *GlobalFlags) error {

	var storage *teststorage.TestStorage
	var err error

	if File != "" {
		storage, err = prom.LoadStorageFromFile(File)
		if err != nil {
			return err
		}

	} else if PromURL != "" {
		storage, err = prom.LoadStorageFromEndpoint(PromURL, Metric)
	} else {
		return errors.New("please specify --file or --name")
	}
	// Create an engine for query evaluation
	engine := promql.NewEngine(promql.EngineOpts{
		Timeout:    10 * time.Second,
		MaxSamples: 50000000,
	})

	fakeAPI := &prom.FakeAPI{Storage: storage, Engine: engine}

	sourceToDestMap, err := mapSourcesToDestinations(fakeAPI, globalFlags.Namespace, Name, Metric)
	if err != nil {
		return err
	}

	if Output == "tree" {
		err = generateAndPrintTree(globalFlags, fakeAPI, sourceToDestMap)
		if err != nil {
			return err
		}
	} else if Output == "authz" {
		err = generateIstioAuthZPolicies(sourceToDestMap, fakeAPI, globalFlags.Namespace, Metric)
		if err != nil {
			return err
		}
	}

	return nil
}

func generateIstioAuthZPolicies(destMap map[string][]*domain.Metadata, api *prom.FakeAPI, namespace string, metric string) error {
	var policies []*v1beta1.AuthorizationPolicy
	for source, destinations := range destMap {
		_, sourcesByName, err := queryAllWorkloads(api, namespace, source, metric)
		if err != nil {
			return err
		}
		var destinationPrinciples []string
		for _, destination := range destinations {
			destinationPrinciples = append(destinationPrinciples, destination.Identity)
		}

		sourceMetrics := sourcesByName[source][0]
		policy := &v1beta1.AuthorizationPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AuthorizationPolicy",
				APIVersion: "security.istio.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      source,
				Namespace: string(sourceMetrics.Metric["source_workload_namespace"]),
			},
			Spec: securityv1beta1.AuthorizationPolicy{
				Selector: &v1beta1api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": source,
					},
				},
				Rules: []*securityv1beta1.Rule{{
					From: []*securityv1beta1.Rule_From{{
						Source: &securityv1beta1.Source{
							Principals: destinationPrinciples,
						},
					}},
				},
				},
				Action: v1.AuthorizationPolicy_AUDIT,
			},
			Status: v1alpha1.IstioStatus{},
		}
		policies = append(policies, policy)
	}

	//marshaller := YamlMarshaller{}
	//yaml, err := marshaller.ToYaml(&policies)
	//if err != nil {
	//	return err
	//}
	//fmt.Println(string(yaml))

	err := printIstioObjects(policies)
	if err != nil {
		return err
	}

	return nil
}

func printIstioObjects(policies []*v1beta1.AuthorizationPolicy) error {
	scheme := runtime.NewScheme()
	serializer := json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme, scheme, json.SerializerOptions{Yaml: true, Pretty: true, Strict: true})

	for _, policy := range policies {
		// Encode the policy to JSON
		yamlData, err := runtime.Encode(serializer, policy)
		if err != nil {
			fmt.Printf("Error encoding to YAML: %v\n", err)
			return err
		}

		fmt.Println(strings.ReplaceAll(string(yamlData), "status: {}\n", ""))
		fmt.Println("---")
	}
	return nil
}

func generateAndPrintTree(globalFlags *GlobalFlags, fakeAPI *prom.FakeAPI, sourceToDestMap map[string][]*domain.Metadata) error {
	rootWorkloads, err := findRootWorkloads(fakeAPI, globalFlags.Namespace, Name, Metric)
	if err != nil {
		return err
	}
	// TODO there is this issue where if you have a circular dependency there is no root so it may hide elements unless they are called from somewhere else

	//convert them to strings
	rootCandidates := make(map[string]*domain.Metadata)
	for _, rootWorkload := range rootWorkloads {
		rootCandidates[rootWorkload] = &domain.Metadata{}
	}

	// Create a root node
	root := domain.NewNode("ROOT", nil)

	// Build the tree
	for workload, metadata := range rootCandidates {
		node := domain.NewNode(workload, metadata)
		domain.BuildTree(node, root, sourceToDestMap, make(map[string]bool))
	}

	// Print the tree
	domain.PrintTree(root, "", true, make(map[string]bool))
	return nil
}
func mapSourcesToDestinations(api *prom.FakeAPI, namespace string, nameFilter string, metric string) (map[string][]*domain.Metadata, error) {
	sourceToDestMap := make(map[string][]*domain.Metadata)

	_, sourcesByName, err := queryAllWorkloads(api, namespace, nameFilter, metric)
	if err != nil {
		return nil, err
	}
	for _, sources := range sourcesByName {
		for _, source := range sources {

			src, srcOK := source.Metric["source_workload"]
			dest, destOK := source.Metric["destination_workload"]
			if !srcOK || !destOK {
				continue
			}
			destinationMetadata := &domain.Metadata{
				Name:      string(dest),
				Namespace: string(source.Metric["destination_workload_namespace"]),
				Identity:  string(source.Metric["destination_principal"]),
				Cluster:   string(source.Metric["destination_cluster"]),
			}
			sourceToDestMap[string(src)] = append(sourceToDestMap[string(src)], destinationMetadata)
		}
	}

	return sourceToDestMap, nil
}

func findRootWorkloads(api *prom.FakeAPI, namespace string, nameFilter string, metric string) ([]string, error) {
	destinations, sources, err := queryAllWorkloads(api, namespace, nameFilter, metric)
	if err != nil {
		return nil, err
	}

	var tier0Workloads []string

	for sourceName, _ := range sources {
		_, ok := destinations[sourceName]
		if !ok {
			tier0Workloads = append(tier0Workloads, sourceName)
		}
	}

	return tier0Workloads, nil
}

func queryAllWorkloads(api *prom.FakeAPI, namespace string, nameFilter string, metric string) (map[string][]*model.Sample, map[string][]*model.Sample, error) {
	var query string
	if namespace != "" || nameFilter != "" {
		var filter string
		if nameFilter != "" {
			filter = "source_workload=~\"" + nameFilter + ".*\", "
		}
		if namespace != "" {
			filter += "source_workload_namespace=\"" + namespace + "\""
		}
		query = fmt.Sprintf("sum(%s{%s}) by (source_workload,source_workload_namespace,source_principal,destination_workload,destination_workload_namespace,destination_principal)", metric, filter)

	} else {
		query = fmt.Sprintf("sum(%s) by (source_workload,source_workload_namespace,source_principal,destination_workload,destination_workload_namespace,destination_principal)", metric)
	}
	output, _, err := api.Query(context.Background(), query, time.Now())
	if err != nil {
		return nil, nil, err
	}
	destinations := map[string][]*model.Sample{}
	sources := map[string][]*model.Sample{}

	switch output.Type() {
	case model.ValScalar:
	case model.ValVector:
		vv := output.(model.Vector)
		for _, sample := range vv {
			sources[string(sample.Metric["source_workload"])] = append(sources[string(sample.Metric["source_workload"])], sample)
			destinations[string(sample.Metric["destination_workload"])] = append(destinations[string(sample.Metric["destination_workload"])], sample)
		}
	case model.ValMatrix:
	default:
		err = fmt.Errorf("unexpected value type %q", output.Type())
	}
	return destinations, sources, nil
}
