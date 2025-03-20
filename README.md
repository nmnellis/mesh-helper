# Mesh Helper

A tool with helpful commands to operate on your Istio service mesh.

```shell
> mesh-helper dependencies --file /tmp/full.json
.
└── istio-ingressgateway
    ├── aged-leaf-v1
    │   ├── cool-butterfly-v1
    │   │   ├── black-field-v1
    │   │   │   ├── icy-sound-v1
    │   │   │   │   └── sparkling-glitter-v1
    │   │   │   │       ├── empty-glade-v1
    │   │   │   │       │   ├── lively-haze-v1
    │   │   │   │       │   ├── old-sunset-v1
    │   │   │   │       │   ├── rough-shape-v1
    │   │   │   │       │   └── wandering-night-v1
    │   │   │   │       ├── floral-fire-v1
    │   │   │   │       └── slashdot.org
    │   │   │   └── morning-fire-v1
    │   │   │       ├── floral-tree-v1
    │   │   │       └── proud-grass-v1
    │   │   └── black-field-v2
```

## Install

```shell
curl -sL https://raw.githubusercontent.com/nmnellis/mesh-helper/main/install.sh | sh -
```

## Use

You have two options to feed data to the cli. You can use promtool to download the data and access it locally. Secondly you can port-forward to the prometheus server and have the cli grab it directly though the API.

### Promtool

You will need [promtool](https://prometheus.io/docs/prometheus/latest/command-line/promtool/)

First you need to get some data:
```shell
# First, get all the metrics we care about. We just 'or' them together.
# You can add more, or fewer, or add filters if you want a subset -- but don't add sum()/rate()
$ promtool query instant 'http://localhost:8080' \
  'istio_request_duration_milliseconds_bucket or istio_requests_total or istio_tcp_received_bytes_total or istio_tcp_sent_bytes_total' --format=json \
   > /tmp/full.json
```

The output takes the following format, if you want to manually write some metrics:
```json
[
  {
    "metric": {
      "__name__": "istio_requests_total",
      "app": "gloo-telemetry-collector-agent",
      "cluster": "kind-alpha",
      "workload_id": "shell.default.kind-alpha"
    },
    "value": [
      1736799648.304,
      "239"
    ]
  }
]
```

* `mesh-helper` with `--file` parameter
```shell
mesh-helper dependencies --file /tmp/full.json
```

### Prometheus API

```shell
kubectl --namespace monitoring port-forward svc/prometheus 9090
```

* `mesh-helper` with `--prom-url` parameter

```shell
mesh-helper dependencies --prom-url http://localhost:9090 --metric istio_tcp_sent_bytes_total
```

## Other Options

* Filter by namespace or name
```shell
mesh-helper dependencies --file /tmp/full.json --namespace bookinfo
```

* Filter by workload name
```
mesh-helper dependencies --file /tmp/full.json --name productpage
```

* Filter by both
```
mesh-helper dependencies --file /tmp/full.json --name productpage --namespace bookinfo
```

## Istio Authorization Policies
* Generate the equivalent AuthorizationPolicies in Istio to enforce zero trust

```shell
dependencies --file /tmp/full.json --output authz --audit true
```

* Output

```yaml
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  creationTimestamp: null
  name: crimson-sky-v3
  namespace: ns-1
spec:
  action: AUDIT
  rules:
  - from:
    - source:
        principals:
        - spiffe://cluster.local/ns/ns-1/sa/broken-shadow
        - spiffe://cluster.local/ns/ns-1/sa/frosty-water
        - spiffe://cluster.local/ns/ns-1/sa/polished-surf
  selector:
    matchLabels:
      app: crimson-sky-v3

---
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  creationTimestamp: null
  name: polished-surf-v1
  namespace: ns-1
spec:
  action: AUDIT
  rules:
  - from:
    - source:
        principals:
        - spiffe://cluster.local/ns/ns-2/sa/dry-firefly
  selector:
    matchLabels:
      app: polished-surf-v1
```