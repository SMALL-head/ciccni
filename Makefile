BINDIR				:= $(CURDIR)/bin 
LDFLAGS             := -s -w
GOFLAGS             := -trimpath
IMAGE_TAG			:= amdv1

gen-proto:
	protoc --proto_path=pkg/apis/cni/pb --go-grpc_out=. --go_out=. pkg/apis/cni/pb/*.proto

bin:
	@mkdir -p $(BINDIR)
	go build -o $(BINDIR) $(GOFLAGS) -ldflags '$(LDFLAGS)' ciccni/cmd/...

clean-bin:
	rm -rf bin

build-image:
	docker build -t registry.cn-shanghai.aliyuncs.com/carl-zyc/ciccni-agent:$(IMAGE_TAG) -f build/images/Dockerfile .