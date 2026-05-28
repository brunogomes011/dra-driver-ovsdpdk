# dra-driver-ovsdpdk

A Kubernetes [Dynamic Resource Allocation (DRA)](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/) driver that exposes OVS-DPDK bridges as schedulable devices. Each allocated device gets a unique per-pod vhost-user socket directory, bind-mounted into the container via CDI.

> **Status:** proof-of-concept / experimentation. OVS port creation is not yet wired; the driver manages socket directories and claim status only.

## How it works

1. An `OvsDpdkResourcePolicy` CRD tells the driver which OVS bridges to advertise on which nodes, and the default permissions (ownership/ACLs) of the socket directories.
2. The driver runs as a DaemonSet. On each node it reconciles matching policies and publishes a `ResourceSlice` listing the bridges as DRA devices (with `AllowMultipleAllocations=true`) (see [DRA consumable-capacity](https://kubernetes.io/blog/2025/09/18/kubernetes-v1-34-dra-consumable-capacity/)).
3. When a pod claims a device, the driver creates a per-pod socket directory on the host, writes a CDI spec that bind-mounts it into the container, and updates `ResourceClaim.Status.Devices` with the mount and socket paths.
4. On pod deletion the directory and CDI spec are removed.

## Configuration — OvsDpdkResourcePolicy

```yaml
apiVersion: ovsdpdk.k8snetworkplumbingwg.io/v1alpha1
kind: OvsDpdkResourcePolicy
metadata:
  name: worker-policy
  namespace: dra-driver-ovsdpdk
spec:
  # nodeSelector restricts which nodes this policy applies to.
  # Omit to apply to all nodes.
  nodeSelector:
    nodeSelectorTerms:
      - matchExpressions:
          - key: ovsdpdk-node
            operator: In
            values: [worker]
  bridges:
    - name: br-dpdk0
    - name: br-dpdk1
  vhostUser:
    hostRootPath: /var/run/ovsdpdk          # root on the host (default)
    containerRootPath: /var/run/ovsdpdk/vhost-user  # root inside the container (default)
    user: openvswitch   # name or numeric UID
    group: qemu         # name or numeric GID
    selinuxLabel: "system_u:object_r:container_file_t:s0"  # optional
    aclUsers:           # optional; granted rwx via setfacl
      - openvswitch
```

| Field | Required | Description |
|---|---|---|
| `bridges[].name` | yes | OVS bridge name to advertise as a DRA device |
| `nodeSelector` | no | Limit to matching nodes; omit for all nodes |
| `vhostUser.hostRootPath` | no | Host root for socket dirs. Default: `/var/run/ovsdpdk` |
| `vhostUser.containerRootPath` | no | Container root for CDI mount. Default: `/var/run/ovsdpdk/vhost-user` |
| `vhostUser.user` | no | Owner of the socket directory (name or UID) |
| `vhostUser.group` | no | Group of the socket directory (name or GID) |
| `vhostUser.selinuxLabel` | no | SELinux label applied to the socket directory |
| `vhostUser.aclUsers` | no | Users granted access via `setfacl` |

## Deploying

### Prerequisites

- Kubernetes ≥ 1.32 with these feature gates enabled on the API server, scheduler, and all kubelets:
  ```
  --feature-gates=DRAConsumableCapacity=true,DRAResourceClaimDeviceStatus=true
  ```
  For kubeadm, set `featureGates` in `KubeletConfiguration`, `KubeSchedulerConfiguration`, and the API server `ClusterConfiguration`. For OpenShift, use the `FeatureGate` CR.
- OVS-DPDK installed on worker nodes with the bridges already created.
- A container registry reachable from the cluster nodes.

### KIND cluster

If you want to quickly experiment with the driver, you can create a kind cluster with:

```bash
kind create cluster --config deployments/kind-config.yaml
```

### Build and push

```bash
export IMAGE_NAME=quay.io/amorenoz/dra-driver-ovsdpdk  # adjust as needed
export IMAGE_TAG=latest
make build-image
podman push "${IMAGE_NAME}:${IMAGE_TAG}"
```

### Deploy

```bash
# CRD, namespace, RBAC, and DaemonSet in one shot:
make deploy

# Or step by step:
kubectl apply -f deployments/crds/
kubectl apply -f deployments/namespace.yaml
kubectl apply -f deployments/rbac.yaml
sed "s|IMAGE|${IMAGE_NAME}:${IMAGE_TAG}|g" deployments/daemonset.yaml | kubectl apply -f -
```

Wait for rollout:

```bash
kubectl rollout status daemonset/dra-driver-ovsdpdk -n dra-driver-ovsdpdk
kubectl logs -n dra-driver-ovsdpdk -l app=dra-driver-ovsdpdk --prefix
```

### Configure policies and label nodes

Edit `deployments/example-policy.yaml` to match your bridges and ownership requirements, then:

```bash
kubectl apply -f deployments/example-policy.yaml
kubectl label node <worker-node> ovsdpdk-node=worker
```

Verify the driver published ResourceSlices:

```bash
kubectl get resourceslices -o wide
```

### Create a DeviceClass

```bash
kubectl apply -f deployments/example-deviceclass.yaml
```

### Consume a device

The recommended pattern is a `ResourceClaimTemplate` so that each pod gets its
own claim. The pod-local claim name (here `vhost`) is used for the socket path,
giving stable, predictable paths across pod restarts and VM migrations:

```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: dpdk-port
spec:
  spec:
    devices:
      requests:
        - name: vhost-port
          exactly:
            deviceClassName: ovsdpdk
            selectors:
              - cel:
                  expression: 'device.attributes["ovsdpdk.k8snetworkplumbingwg.io"].bridgeName == "br-dpdk0"'
---
apiVersion: v1
kind: Pod
metadata:
  name: my-dpdk-pod
spec:
  restartPolicy: Never
  resourceClaims:
    - name: vhost
      resourceClaimTemplateName: dpdk-port
  containers:
    - name: app
      image: quay.io/fedora/fedora-minimal:latest
      command: ["/bin/bash"]
      args:
        - "-c"
        - "sleep INF"
      resources:
        claims:
          - name: vhost
```

The CEL selector pins scheduling to the node that owns `br-dpdk0`. After the
pod starts, inspect the claim status for the exact socket paths:

```bash
kubectl get resourceclaim

```
```
NAME                      STATE                AGE
my-dpdk-pod-vhost-p5bzb   allocated,reserved   7m58s
```
```bash
kubectl get resourceclaim my-dpdk-pod-vhost-p6bzb \
  -o jsonpath='{.status.devices[0].data}' | jq .
```

```json
{
  "bridgeName": "br-dpdk0",
  "cdiDeviceID": "ovsdpdk.k8snetworkplumbingwg.io/vhost-user=aaa85ca7",
  "mount": {
    "containerDir": "/var/run/ovsdpdk/vhost-user/vhost",
    "hostDir": "/var/run/ovsdpdk/c362b1d7-d4ea-4efe-9e90-e4cd83131baf/vhost"
  },
  "socket": {
    "containerPath": "/var/run/ovsdpdk/vhost-user/vhost/vhost.sock",
    "hostPath": "/var/run/ovsdpdk/c362b1d7-d4ea-4efe-9e90-e4cd83131baf/vhost/vhost.sock"
  }
}
```

> **Hand-written claims**: if you create a `ResourceClaim` directly (without a
> template), the driver uses the claim's name for the socket path.

### Uninstall

```bash
make undeploy
# also remove any policies and the DeviceClass:
kubectl delete -f deployments/example-policy.yaml
kubectl delete -f deployments/example-deviceclass.yaml
```

## Development

```bash
make build    # compile
make test     # unit tests
make check    # vet + lint
make generate # regenerate CRD manifests and deepcopy
```

## Device metadata (KEP-5304 DownwardAPI)

When the driver is started with `--enable-device-metadata` (or `ENABLE_DEVICE_METADATA=true`), it uses the built-in support in `k8s.io/dynamic-resource-allocation` to write a versioned metadata JSON file for each prepared device and bind-mount it read-only into the container at the standard KEP-5304 path:

```
/var/run/kubernetes.io/dra-device-attributes/<pod-claim-name>/<request-name>/metadata.json
```

The file is a JSON stream in the `metadata.resource.k8s.io/v1alpha1` format. It contains two device attributes:

| Attribute key | Value |
|---|---|
| `vhost-user-path` | Container-side path of the vhost-user socket |


## Multus-mode mutating webhook
For workloads that cannot currently request DRA resources (e.g: Kubevirt VMs), a mutating webhook and a dummy CNI binary are available.

The cni binary is called `ovsdpdk` and is pretty much a no-op. It just implements the CNI spec to keep Multus happy.
The webhook works similar to the [network-resource-injector webhook](https://github.com/k8snetworkplumbingwg/network-resources-injector). 

If it finds a Pod that is requesting a network of type `ovsdpdk` and the `NetworkAttachmentDefinition` contains the special annotation `ovsdpdk.io/resourceClaimTemplate` pointing to a valid `ResourceClaimTemplate`, it injects a claim to all containers
in the pod requesting a resource provided by that `ResourceClaimTemplate`. The pod-claim-name will be the same as the network interface name or auto-generated.

For example, given a `NetworkAttachmentDefinition` such as:
```yaml
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: ovs-net-dra
  annotations:
    ovsdpdk.io/resourceClaimTemplate: ovsdpdk
spec:
  config: '{
      "type": "ovsdpdk"
    }'
```

A pod containing:

```yaml
apiVersion: v1
kind: Pod
metadata:
  k8s.v1.cni.cncf.io/networks: '[{"name":"ovs-net-dra", "interface":"my-vhost-iface"}]'
# [...]
```

Will be mutated to have:

```yaml
apiVersion: v1
kind: Pod
spec:
  containers:
  - name: foo
    resources:
      claims:
      - name: my-vhost-iface
  # [...]
  resourceClaims:
  - name: my-vhost-iface
    resourceClaimTemplateName: ovsdpdk
    resources:
```

### TODO List
This is very early stage of development. Planned features are:
- [ ] OVS support including: bridge monitorind, port creation / deletion and uplink detection
- [x] Claim-injection webhook from a NetworkAttachmentDefinition for easy integration with Multus-based setups.
- [ ] DevicePlugin server to expose topology of detected uplink interfaces
- [ ] Networking features: VLAN, QoS, custom MTU
- [ ] Persistency across driver reboots
- [x] DRA Downward API (KEP-5304) — implemented via `--enable-device-metadata`

