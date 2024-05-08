# 部署
## 一些编码步骤
```
kubebuilder init --domain ndsl.cn
kubebuilder create api --group distri-infer --version v1 --kind Pipeline
```

> crd `pipelines.distri-infer.ndsl.cn`

修改types.go
`make manifests`

写template 写解析渲染

todo:
写reconcil逻辑

## 集群前置条件
1. nfs服务器配置
要在机器192.168.20.235上配置 NFS 服务器端并共享目录`/home/aiedge/csiTest`，需要执行以下步骤：

- **安装 NFS 服务器软件：**
   使用以下命令安装 NFS 服务器：

   ```bash
   sudo apt-get update
   sudo apt-get install nfs-kernel-server
   ```

- **配置 NFS 服务器：**
打开 NFS 服务器的配置文件，通常为 `/etc/exports`：`sudo nano /etc/exports`

在文件的末尾添加以下行，以共享`/home/aiedge/csiTest`目录：

```bash
/home/aiedge/csiTest *(rw,sync,no_root_squash)
```
**这个配置允许所有主机（`*`）以读写（`rw`）的方式访问共享目录。**【基于安全的最佳配置 见csi-nfs,md】

- **重启 NFS 服务：**
完成配置后，重启 NFS 服务以使更改生效：

```bash
sudo systemctl restart nfs-kernel-server   # 对于基于 systemd 的系统
```
- **验证共享配置：**
使用以下命令验证 NFS 服务器是否正在运行并已正确配置：

```bash
sudo systemctl status nfs-kernel-server   # 检查服务状态
showmount -e 192.168.20.235               # 显示可用的 NFS 共享
```
现在，你已经在192.168.20.235机器上配置好了 NFS 服务器端，并共享了`/home/aiedge/csiTest`目录。其他主机可以使用 NFS 客户端挂载这个共享目录。

2. nfs-csi部署
```bash
git clone https://github.com/kubernetes-csi/csi-driver-nfs.git
cd csi-driver-nfs
./deploy/install-driver.sh v4.1.0 local #表示用本地yaml部署
```

镜像问题
所需要的镜像：
- "registry.k8s.io/sig-storage/csi-provisioner:v3.2.0"
- "registry.k8s.io/sig-storage/csi-node-driver-registrar:v2.5.1"
- "registry.k8s.io/sig-storage/nfsplugin:v4.1.0"
- "registry.k8s.io/sig-storage/livenessprobe:v2.7.0"
解决：`ctr -n k8s.io image import /home/aiedge/csi_images.tar`【251节点上】

查看
`kubectl get po -Aowide | grep csi`
出现以下pod且running则此步成功
```bash
csi-nfs-controller-78d56d9785-nnbcc    csi-nfs-node-249fj                     csi-nfs-node-lc6qj                     csi-nfs-node-rcwqs                     
```

> csi-nfs的具体使用部署，可见`csi-nfs.md`

3. pipeline命名空间
`kubectl apply -f ./config/samples/namespace.yaml`
`kubectl get ns`

## 本地测试
```bash
make manifests
make install
kubectl get crd

kubectl apply -f ./config/samples/namespace.yaml
kubectl get ns

go run ./cmd/main.go
kubectl apply -f ./config/samples/distri-infer_v1_pipeline.yaml

```
删除测试
```bash
kubectl delete deployment facedetection-2 -n pipeline
```

检查pod 内部 模型文件、env 
```bash
aiedge@xx-test-master235:~/project/distributed-inference/pipeline-operator$ kubectl exec -it  facedetection-2-77475974bb-wlxg4 -n pipeline -- /bin/bash
root@facedetection-2-77475974bb-wlxg4:/# ls data/
facedetection-1.txt  facedetection-2.txt
root@facedetection-2-77475974bb-wlxg4:/# env | grep begin
begin=false
root@facedetection-2-77475974bb-wlxg4:/# env | grep listenPort
listenPort=9080
root@facedetection-2-77475974bb-wlxg4:/# env | grep args
args=map[]
root@facedetection-2-77475974bb-wlxg4:/# env | grep dst
dst=facedetection-1
root@facedetection-2-77475974bb-wlxg4:/# env | grep modelPath
modelPath=/data/facedetection-2
```

## 封装镜像测试
`make docker-build docker-push IMG=registry.cn-hangzhou.aliyuncs.com/ndsl/pipelineoperator:v1`

`kubectl apply -f ./config/samples/namespace.yaml`
`kubectl get ns`

根据 `IMG` 指定的镜像将控制器部署到集群中:
`make deploy IMG=registry.cn-hangzhou.aliyuncs.com/ndsl/pipelineoperator:v1`
```bash
kubectl get crd
kubectl get pipeline
```
查看控制器是否正常ready：
在初始化项目时，kubebuilder 会自动根据项目名称创建一个 Namespace，如本项目中的pipeline-operator-system
`kubectl get deployment -n pipeline-operator-system`
`kubectl get pod -n pipeline-operator-system`
输出类似`pipeline-operator-system   pipeline-operator-controller-manager   1/1     1            1           44s`
部署cr流水线：
```bash
kubectl apply -f ./config/samples/distri-infer_v1_pipeline.yaml
```

查看日志
`kubectl logs -f pipeline-operator-controller-manager-77f4454d87-zfh2r -n pipeline-operator-system `

删除流水线
`kubectl delete pipeline`

