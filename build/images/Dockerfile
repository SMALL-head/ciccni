FROM registry.cn-shanghai.aliyuncs.com/carl-zyc/openvswitch:2.13.8

USER root

# 脚本拷贝至/usr/local/bin下就可以直接调用了
COPY ./build/images/scripts/* /usr/local/bin/
COPY ./bin/* /usr/local/bin/