package cmd

import (
	"context"
	"errors"
	"fmt"
	"github.com/nmnellis/mesh-helper/internal/domain"
	"github.com/nmnellis/mesh-helper/internal/prom"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/util/teststorage"
	"github.com/spf13/cobra"
	"istio.io/api/meta/v1alpha1"
	v2 "istio.io/api/networking/v1"
	v1 "istio.io/api/security/v1"
	securityv1beta1 "istio.io/api/security/v1beta1"
	v1beta1api "istio.io/api/type/v1beta1"
	networkingv1 "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/client-go/pkg/apis/security/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"strings"
	"time"
)

type DependenciesArgs struct {
	Name      string
	File      string
	Output    string
	PromURL   string
	Audit     bool
	Metric    string
	Namespace string
}

func dependenciesCmd() *cobra.Command {
	depArgs := &DependenciesArgs{}
	cmd := &cobra.Command{
		Use:     "dependencies",
		Aliases: []string{"dep"},
		Short:   "List application dependencies",
		Long:    ` `,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDependencies(depArgs)
		},
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		SilenceUsage: true,
	}
	cmd.Flags().StringVarP(&depArgs.Output, "output", "o", "tree", "Output Format (tree, authz, sidecar)")
	cmd.Flags().StringVarP(&depArgs.File, "file", "f", "", "Read from a prometheus formatted input file")
	cmd.Flags().StringVar(&depArgs.Name, "name", "", "Filter for workload by name")
	cmd.Flags().StringVar(&depArgs.PromURL, "prom-url", "", "Call prometheus directly to fetch data")
	cmd.Flags().BoolVar(&depArgs.Audit, "audit", true, "Audit traffic rather than deny")
	cmd.Flags().StringVar(&depArgs.Metric, "metric", "istio_tcp_sent_bytes_total", "Metric to grab dependency tree (istio_tcp_sent_bytes_total, istio_requests_total)")
	cmd.Flags().StringVarP(&depArgs.Namespace, "namespace", "n", "", "Namespace to runDependencies the command in.")
	return cmd
}

func runDependencies(args *DependenciesArgs) error {

	var storage *teststorage.TestStorage
	var err error

	if args.File != "" {
		storage, err = prom.LoadStorageFromFile(args.File)
		if err != nil {
			return err
		}

	} else if args.PromURL != "" {
		storage, err = prom.LoadStorageFromEndpoint(args.PromURL, args.Metric)
	} else {
		return errors.New("please specify --file or --name")
	}
	// Create an engine for query evaluation
	engine := promql.NewEngine(promql.EngineOpts{
		Timeout:    10 * time.Second,
		MaxSamples: 50000000,
	})

	fakeAPI := &prom.FakeAPI{Storage: storage, Engine: engine}

	sourceToDestMap, err := mapSourcesToDestinations(fakeAPI, args.Namespace, args.Name, args.Metric)
	if err != nil {
		return err
	}

	if args.Output == "tree" {
		err = generateAndPrintTree(fakeAPI, sourceToDestMap, args)
		if err != nil {
			return err
		}
	} else if args.Output == "authz" {
		err = generateIstioAuthZPolicies(sourceToDestMap, fakeAPI, args.Namespace, args.Metric)
		if err != nil {
			return err
		}
	} else if args.Output == "sidecar" {
		err = generateIstioSidecar(sourceToDestMap, fakeAPI, args.Namespace, args.Metric)
		if err != nil {
			return err
		}
	}

	return nil
}

func generateIstioSidecar(destMap map[string][]*domain.Metadata, api *prom.FakeAPI, namespace string, metric string) error {
	var policies []runtime.Object
	for source, destinations := range destMap {
		_, sourcesByName, err := queryAllWorkloads(api, namespace, source, metric)
		if err != nil {
			return err
		}
		var destinationWorkloads []string
		for _, destination := range destinations {
			destinationWorkloads = append(destinationWorkloads, fmt.Sprintf("*/%s.%s.svc.cluster.local", destination.Name, destination.Namespace))
		}

		sourceMetrics := sourcesByName[source][0]
		policy := &networkingv1.Sidecar{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Sidecar",
				APIVersion: "security.istio.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      source,
				Namespace: string(sourceMetrics.Metric["source_workload_namespace"]),
			},
			Spec: v2.Sidecar{
				Egress: []*v2.IstioEgressListener{
					{
						Hosts: destinationWorkloads,
					},
				},
				WorkloadSelector: &v2.WorkloadSelector{
					Labels: map[string]string{
						"app": source,
					},
				},
			},
			Status: v1alpha1.IstioStatus{},
		}
		policies = append(policies, policy)
	}

	err := printIstioObjects(policies)
	if err != nil {
		return err
	}
	return nil
}

func generateIstioAuthZPolicies(destMap map[string][]*domain.Metadata, api *prom.FakeAPI, namespace string, metric string) error {
	var policies []runtime.Object
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

	err := printIstioObjects(policies)
	if err != nil {
		return err
	}

	return nil
}

func printIstioObjects(policies []runtime.Object) error {
	scheme := runtime.NewScheme()
	serializer := json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme, scheme, json.SerializerOptions{Yaml: true, Pretty: true, Strict: true})

	for _, policy := range policies {
		// Encode the policy to JSON
		yamlData, err := runtime.Encode(serializer, policy)
		if err != nil {
			fmt.Printf("Error encoding to YAML: %v\n", err)
			return err
		}
		output := strings.ReplaceAll(string(yamlData), "status: {}\n", "")
		output = strings.ReplaceAll(output, "  creationTimestamp: null\n", "")
		fmt.Println(output)
		fmt.Println("---")
	}
	return nil
}

func generateAndPrintTree(fakeAPI *prom.FakeAPI, sourceToDestMap map[string][]*domain.Metadata, args *DependenciesArgs) error {
	rootWorkloads, err := findRootWorkloads(fakeAPI, args.Namespace, args.Name, args.Metric)
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
