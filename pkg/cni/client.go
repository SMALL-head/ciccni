package cni

import (
	"ciccni/pkg/apis/cni/pb"
	"ciccni/pkg/logUtils"
	"context"
	"github.com/containernetworking/cni/pkg/skel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"net"
	"os"
)

// CICCNISocketAddr rpc类unix域套接字地址
const CICCNISocketAddr = "/var/run/ciccni/cni.sock"

var withClient = rpcClient

func rpcClient(f func(client pb.CniClient) error) error {
	conn, err := grpc.Dial(
		CICCNISocketAddr,
		//grpc.WithInsecure(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (conn net.Conn, e error) {
			return net.Dial("unix", addr)
		}),
	)
	if err != nil {
		logUtils.Log.Errorf("[client.go]-[rpcClient]-rpc连接失败")
		return err
	}
	defer conn.Close()
	return f(pb.NewCniClient(conn))
}

func CmdAdd(arg *skel.CmdArgs) error {
	return withClient(func(client pb.CniClient) error {
		request := &pb.CniCmdRequest{CniArgs: &pb.CniCmdArgs{
			ContainerId:          arg.ContainerID,
			Ifname:               arg.IfName,
			Args:                 arg.Args,
			Netns:                arg.Netns,
			NetworkConfiguration: arg.StdinData,
			Path:                 arg.Path,
		}}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		resp, err := client.CmdAdd(ctx, request)
		if err != nil {

			logUtils.Log.Errorf("[client.go]-[CmdAdd]-调用rpc请求CmdAdd失败, err=%s", err)
			return err
		}

		logUtils.Log.Infof("[client.go]-[CmdAdd]-来自rpcServer的resp: %s", resp.CniResult)
		os.Stdout.Write(resp.CniResult) // cni处理返回结果必须要返回给stdout

		return nil
	})
}

func CmdCheck(arg *skel.CmdArgs) error {
	return nil

}

func CmdDel(arg *skel.CmdArgs) error {
	return nil
}
