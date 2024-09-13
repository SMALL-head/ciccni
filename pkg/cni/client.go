package cni

import (
	"ciccni/pkg/apis/cni/pb"
	"ciccni/pkg/logUtils"
	"context"
	"fmt"
	"net"
	"os"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// CICCNISocketAddr rpc类unix域套接字地址
const CICCNISocketAddr = "/var/run/ciccni/cni.sock"

type Action int

const (
	ActionAdd Action = iota
	ActionCheck
	ActionDel
)

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

func (a Action) Request(arg *skel.CmdArgs) error {
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
		var err error
		var resp *pb.CniCmdResponse

		switch a {
		case ActionAdd:
			resp, err = client.CmdAdd(ctx, request)
		case ActionDel:
			resp, err = client.CmdDel(ctx, request)
		case ActionCheck:
			resp, err = client.CmdCheck(ctx, request)
		}

		if status.Code(err) == codes.Unimplemented {
			return &types.Error{
				Code:    uint(pb.ErrorCode_INCOMPATIBLE_API_VERSION),
				Msg:     "incompatible CNI API version between client (antrea-cni) and server (antrea-agent)",
				Details: fmt.Sprintf("service or method unimplemented by gRPC server: %v", err.Error()),
			}
		} else if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
			// network errors, could be transient.
			return &types.Error{
				Code: uint(pb.ErrorCode_TRY_AGAIN_LATER),
				Msg:  err.Error(),
			}
		} else if err != nil { // all other RPC errors.
			return &types.Error{
				Code: uint(pb.ErrorCode_UNKNOWN_RPC_ERROR),
				Msg:  err.Error(),
			}
		}

		// Handle errors during CNI execution.
		if resp.Error != nil {
			return &types.Error{
				Code: uint(resp.Error.Code),
				Msg:  resp.Error.Message,
			}
		}
		os.Stdout.Write(resp.CniResult) // cni处理返回结果必须要返回给stdout

		return nil
	})

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
		// if err != nil {

		// 	logUtils.Log.Errorf("[client.go]-[CmdAdd]-调用rpc请求CmdAdd失败, err=%s", err)
		// 	return err
		// }

		if status.Code(err) == codes.Unimplemented {
			return &types.Error{
				Code:    uint(pb.ErrorCode_INCOMPATIBLE_API_VERSION),
				Msg:     "incompatible CNI API version between client (antrea-cni) and server (antrea-agent)",
				Details: fmt.Sprintf("service or method unimplemented by gRPC server: %v", err.Error()),
			}
		} else if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
			// network errors, could be transient.
			return &types.Error{
				Code: uint(pb.ErrorCode_TRY_AGAIN_LATER),
				Msg:  err.Error(),
			}
		} else if err != nil { // all other RPC errors.
			return &types.Error{
				Code: uint(pb.ErrorCode_UNKNOWN_RPC_ERROR),
				Msg:  err.Error(),
			}
		}

		// Handle errors during CNI execution.
		if resp.Error != nil {
			return &types.Error{
				Code: uint(resp.Error.Code),
				Msg:  resp.Error.Message,
			}
		}
		os.Stdout.Write(resp.CniResult) // cni处理返回结果必须要返回给stdout

		return nil
	})

}

func CmdDel(arg *skel.CmdArgs) error {
	return nil
}
