docker run -it --name ovs-test --privileged=true --net=host -v /var/run/openvswitch:/var/run/openvswitch registry.cn-shanghai.aliyuncs.com/carl-zyc/openvswitch:2.13.8 /bin/bash
docker rm ovs-test

/usr/share/openvswitch/scripts/ovs-ctl --system-id=random start --db-file="/var/run/openvswitch/conf.db"