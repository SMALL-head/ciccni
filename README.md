# 项目说明

本项目是仿照 Antrea v0.1.0 写的简易版的代码，是一个使用 ovs 进行 pod 间网络连通的 cni，具体结构可以参考 Antrea v0.1.0 的 doc。与原版相比，

- 简化了流表项配置，只实现了 pod to pod, pod to external 的连通（也就是核心的容器网络连通部分），并且增加了大量的日志，可以方便用户进行调试，从而学习到 cni 的配置流程。
- yaml 文件配置可以在更高版本的 k8s 上运行（v0.1.0 的 antrea 版本号稍低，我写的这份经过测试在 ubuntu+k8s_1.23.5 可以运行）

跨主机的 pod 交互采用 vxlan 进行交互，pod to external 的通过 iptables 的 SNAT 进行交互。

# 环境安装

## 安装 k8s

推荐版本为 1.23.x。由于我在 cni 配置文件中写了 version 为`0.3.0`，某些 k8s 集群支持的最低版本为`0.4.0`，例如 1.25.x 的 k8s，因此可能会导致 cni 安装失败
安装 k8s 集群的文档可以参见<a>https://www.yuque.com/carlson-zyc/sni76l/xzxqrf16ebgfpg7s?singleDoc# </a>《k8s 安装极简版教程》

## 安装 ovs

在 ubuntu 中直接使用
`sudo apt install openvswitch-switch`即可

# 部署方法

拷贝 build/yaml/ciccni.yaml 文件，里面可能需要更改 ciccni-agent 的镜像版本号
然后在集群中使用`kubectl apply -f ciccni.yaml`

# 开发调试过程

## 打包

修改完代码之后，执行下面的语句进行打包镜像，并上传至远程镜像仓库中

```bash
# 在项目根目录下进行
make clean-bin bin                                                   # 编译文件
docker build -t {image_name}:{version} -f build/images/Dockerfile .  # 构建cni agent的镜像
docker push {image_name}:{version}                                   # 上传镜像
```
