package cniserver

import (
	"ciccni/pkg/apis/cni/pb"
	"ciccni/pkg/logUtils"
	"context"
	"google.golang.org/grpc"
	"net"
	"os"
)

type CniServer struct {
	pb.UnimplementedCniServer
	socketAddr string
}

func New(cniSocket string) *CniServer {
	return &CniServer{socketAddr: cniSocket}
}

// Run 启动cniServer，主要是将rpc服务器绑定到unix域套接字上
func (cniServer *CniServer) Run(stopCh <-chan struct{}) {
	logUtils.Log.Infoln("[cniserver.go]-[Run]-启动cniServer")
	defer logUtils.Log.Infoln("[cniserver.go]-[Run]-关闭cniServer")
	server := grpc.NewServer()
	pb.RegisterCniServer(server, cniServer)

	// 将server连接到unix域套接字
	_ = os.Remove(cniServer.socketAddr) // 提前删除，防止出现bind error
	listener, err := net.Listen("unix", cniServer.socketAddr)
	if err != nil {
		logUtils.Log.Errorf("[cniserver.go]-[Run]-连接到unix://%s错误，err=%v", cniServer.socketAddr, err)
		os.Exit(1)
	}
	go func() {
		if err := server.Serve(listener); err != nil {
			logUtils.Log.Errorf("Failed to serve connections: %v", err)
		}
	}()

	<-stopCh
}

func (cniServer *CniServer) CmdAdd(ctx context.Context, request *pb.CniCmdRequest) (*pb.CniCmdResponse, error) {

	resp := &pb.CniCmdResponse{CniResult: nil}
	return resp, nil
}
