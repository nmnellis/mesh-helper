package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/fatih/color"
	"github.com/nmnellis/mesh-helper/internal/domain/envoy"
	"github.com/rodaine/table"
	"github.com/spf13/cobra"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/bug-report/pkg/common"
	"istio.io/istio/tools/bug-report/pkg/kubeclient"
	"istio.io/istio/tools/bug-report/pkg/kubectlcmd"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"strconv"
	"strings"
	"time"
)

type EndpointsArgs struct {
	CommandTimeout time.Duration
	DeploymentName string
	PodName        string
	Namespace      string
}

func endpointsCmd(ctx context.Context, globalFlags *GlobalFlags) *cobra.Command {
	log.ErrorEnabled()
	endpointArgs := &EndpointsArgs{}
	cmd := &cobra.Command{
		Use:     "endpoints",
		Aliases: []string{"endpts", "e"},
		Short:   "Print endpoints of specific applications",
		Long:    ` `,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEndpointsCMD(ctx, globalFlags, endpointArgs)
		},
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		SilenceUsage: true,
	}
	// set global CLI flags
	globalFlags.AddToFlags(cmd.PersistentFlags())
	cmd.Flags().DurationVarP(&endpointArgs.CommandTimeout, "timeout", "t", time.Minute*5, "Timeout")
	cmd.Flags().StringVar(&endpointArgs.PodName, "pod-name", "", "Name of pod to gather endpoints")
	cmd.Flags().StringVarP(&endpointArgs.DeploymentName, "deployment-name", "d", "", "Name of deployment to gather endpoints from all pods")
	cmd.Flags().StringVarP(&endpointArgs.Namespace, "namespace", "n", "", "Namespace to runDependencies the command in.")

	cmd.MarkFlagRequired("namespace")
	return cmd
}

func runEndpointsCMD(ctx context.Context, globalFlags *GlobalFlags, args *EndpointsArgs) error {

	// this disables Istio from printing its info logs
	err := disableIstioInfoLogging()
	if err != nil {
		return err
	}

	restConfig, clientset, err := kubeclient.New(globalFlags.KubeConfigPath, globalFlags.KubeContext)
	if err != nil {
		return fmt.Errorf("could not initialize k8s client: %s ", err)
	}
	client, err := kube.NewCLIClient(kube.NewClientConfigForRestConfig(restConfig))
	if err != nil {
		return err
	}

	clusterResourcesCtx, getClusterResourcesCancel := context.WithTimeout(ctx, args.CommandTimeout)
	curTime := time.Now()
	defer func() {
		if time.Until(curTime.Add(args.CommandTimeout)) < 0 {
			message := "Timeout when running bug report command, please using --include or --exclude to filter"
			common.LogAndPrintf("%s", message)
		}
		getClusterResourcesCancel()
	}()

	// first find the pods associated with the query
	pods, err := findPods(clusterResourcesCtx, clientset, args)
	if err != nil {
		return err
	}

	endpointInfo, err := getEndpointInformation(pods, client)
	if err != nil {
		return err
	}

	if err := printEndpointInfo(endpointInfo); err != nil {
		return err
	}

	return nil
}

func disableIstioInfoLogging() error {
	logOpts := log.DefaultOptions()
	logOpts.SetDefaultOutputLevel("default", log.ErrorLevel)
	err := log.Configure(logOpts)
	if err != nil {
		return err
	}
	return nil
}

func printEndpointInfo(clusters map[string]*envoy.Clusters) error {

	headerFmt := color.New(color.FgGreen, color.Underline).SprintfFunc()
	columnFmt := color.New(color.FgYellow).SprintfFunc()

	var noEndpointsPods []string
	for namespacePodName, cluster := range clusters {

		tbl := table.New("Cluster", "Endpoint", "Port", "Rq Success", "Rq Error", "Cx Active", "Cx Connect Fail", "Priority")
		tbl.WithHeaderFormatter(headerFmt).WithFirstColumnFormatter(columnFmt)
		var rows = 0
		for _, s := range cluster.ClusterStatuses {
			nameSplit := strings.Split(s.Name, "|")
			if nameSplit[0] == "outbound" {
				for _, hs := range s.HostStatuses {
					statsMap := map[string]string{}
					for _, stat := range hs.Stats {
						if stat.Value != "" {
							value, err := strconv.Atoi(stat.Value)
							if err != nil {
								// just print the string
								if stat.Value != "" {
									//fmt.Printf("\t\t%s: %s\n", stat.Name, stat.Value)
									statsMap[stat.Name] = stat.Value
								}
							} else {
								if value != 0 {
									//fmt.Printf("\t\t%s: %s\n", stat.Name, stat.Value)
									statsMap[stat.Name] = stat.Value
								}
							}
						}
					}
					if len(statsMap) > 0 {
						tbl.AddRow(nameSplit[3], hs.Address.SocketAddress.Address, hs.Address.SocketAddress.PortValue, statsMap["rq_success"], statsMap["rq_error"], statsMap["cx_active"], statsMap["cx_connect_fail"], statsMap["priority"])
						rows++
					}
				}

			}
		}
		if rows > 0 {
			fmt.Printf("%s\n--------------------------------------------------------------------------------------------\n", namespacePodName)
			tbl.Print()
			fmt.Println("\n")
		} else {
			noEndpointsPods = append(noEndpointsPods, namespacePodName)
		}
	}
	for _, pod := range noEndpointsPods {
		fmt.Printf("\nNo outbound active endpoints found for %s", pod)
	}

	return nil
}

func getEndpointInformation(pods map[string]*corev1.Pod, client kube.CLIClient) (map[string]*envoy.Clusters, error) {
	podEndpoints := map[string]*envoy.Clusters{}

	for namespacePodName, pod := range pods {
		// find if istio-proxy container
		if !containsProxyContainer(pod) {
			fmt.Printf("%s is not a proxy container\n", namespacePodName)
			continue
		}

		// kubectl exec to endpoint to get stats
		stats, err := getEndpointsFromPod(pod, client)
		if err != nil {
			fmt.Printf("Error getting endpoints from pod: %s %s\n", namespacePodName, err)
		}

		clusters, err := parseStatsIntoEndpointInfo(stats)
		if err != nil {
			fmt.Printf("Error getting endpoints from pod: %s %s\n", namespacePodName, err)
		}
		podEndpoints[namespacePodName] = clusters
	}

	return podEndpoints, nil
}

func parseStatsIntoEndpointInfo(stats string) (*envoy.Clusters, error) {
	var clusters envoy.Clusters
	err := json.Unmarshal([]byte(stats), &clusters)
	if err != nil {
		return nil, err
	}
	return &clusters, nil
}

func getEndpointsFromPod(pod *corev1.Pod, client kube.CLIClient) (string, error) {
	runner := kubectlcmd.NewRunner(-1)
	runner.SetClient(client)
	response, err := runner.EnvoyGet(pod.Namespace, pod.Name, "clusters?format=json", false)
	if err != nil {
		return "", err
	}
	return response, nil
}

func containsProxyContainer(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == "istio-proxy" {
			return true
		}
	}

	return false
}

// Pod maps a pod name to its Pod info. The key is namespace/pod-name.
func findPods(ctx context.Context, clientset *kubernetes.Clientset, args *EndpointsArgs) (map[string]*corev1.Pod, error) {
	pods := map[string]*corev1.Pod{}
	if args.PodName != "" {
		pod, err := clientset.CoreV1().Pods(args.Namespace).Get(ctx, args.PodName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		pods[podNameNamespace(pod.Name, pod.Namespace)] = pod
	} else if args.DeploymentName != "" {
		// Get the deployment
		deployment, err := clientset.AppsV1().Deployments(args.Namespace).Get(ctx, args.DeploymentName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		// List the pods with the same labels as the deployment
		labelSelector := metav1.FormatLabelSelector(deployment.Spec.Selector)
		deploymentPods, err := clientset.CoreV1().Pods(args.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return nil, err
		}

		for _, pod := range deploymentPods.Items {
			pods[podNameNamespace(pod.Name, pod.Namespace)] = &pod
		}
	} else { // namespace only
		podsList, err := clientset.CoreV1().Pods(args.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, pod := range podsList.Items {
			pods[podNameNamespace(pod.Name, pod.Namespace)] = &pod
		}
	}
	fmt.Println("found ", len(pods), "pods")
	return pods, nil
}

func podNameNamespace(name string, namespace string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
