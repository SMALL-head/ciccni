# FROM golang:1.22.0 as builder
# WORKDIR /ciccni
# COPY . .
# RUN go env -w GOPROXY=https://goproxy.cn,direct && \
#     CGO_ENABLE=0 go build -o main pkg/ovs/openflow/main/ovs_test_main.go

FROM registry.cn-shanghai.aliyuncs.com/carl-zyc/openvswitch:2.13.8
WORKDIR /app
# COPY --from=builder /ciccni/main .
COPY bin/* /app/
ENTRYPOINT /app/ovs-test