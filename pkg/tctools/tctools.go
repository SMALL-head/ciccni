package tctools

import (
	"errors"
	"net"
	"regexp"
	"strconv"

	"github.com/florianl/go-tc"
	"github.com/florianl/go-tc/core"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
	"k8s.io/klog"
)

/* Flags */
const (
	TC_U32_TERMINAL  = uint8(1)
	TC_U32_OFFSET    = uint8(2)
	TC_U32_VAROFFSET = uint8(4)
	TC_U32_EAT       = uint8(8)
)

const (
	TCA_CLS_FLAGS_SKIP_HW   = uint32(1 << 0) /* don't offload filter to HW */
	TCA_CLS_FLAGS_SKIP_SW   = uint32(1 << 1) /* don't use filter in SW */
	TCA_CLS_FLAGS_IN_HW     = uint32(1 << 2) /* filter is offloaded to HW */
	TCA_CLS_FLAGS_NOT_IN_HW = uint32(1 << 3) /* filter isn't offloaded to HW */
	TCA_CLS_FLAGS_VERBOSE   = uint32(1 << 4) /* verbose logging */
)

/* Protocol */
type Protocol uint16

const (
	ProtocolIP Protocol = 8
)

var tcnl *tc.Tc

func init() {
	var err error
	tcnl, err = tc.Open(&tc.Config{})
	if err != nil {
		klog.Fatalf("can't open rtnetlink, err = %v", err)
	}

	// For enhanced error messages from the kernel, it is recommended to set
	// option `NETLINK_EXT_ACK`, which is supported since 4.12 kernel.
	//
	// If not supported, `unix.ENOPROTOOPT` is returned.
	if err := tcnl.SetOption(netlink.ExtendedAcknowledge, true); err != nil {
		klog.Warningf("EXT_ACK set failed, err = %v", err)
	}
}

func DeleteRootFromInterface(ifName string) error {
	return nil
}

// AddHTBToInterface  相当于运行
// $TC qdisc add dev {ifName} root handle 1:0 htb
func addHTBToInterface(ifName string) error {
	ifByName, err := net.InterfaceByName(ifName)
	if err != nil {
		klog.Errorf("未找到名为%s的interface", ifName)
		return err
	}

	qdiscHTB := createHTBObject(uint32(ifByName.Index),
		core.BuildHandle(0x1, 0x0),
		tc.HandleRoot,
		nil, &tc.HtbGlob{Version: 3, Rate2Quantum: 10})

	tcnlInNs, err := tc.Open(&tc.Config{})
	if err != nil {
		klog.Errorf("err creating tcnl, err = %s", err)
		return err
	}
	defer tcnlInNs.Close()
	if err := tcnl.SetOption(netlink.ExtendedAcknowledge, true); err != nil {
		klog.Warningf("EXT_ACK set failed, err = %v", err)
	}

	if err := tcnlInNs.Qdisc().Add(qdiscHTB); err != nil {
		return err
	}
	return nil
}

// addHTBClass something like
// $TC class add dev {ifName} parent {parent} classid {classid} htb rate {limit} ceil {limit+burst}
func addHTBClass(ifName string, parent uint32, classid uint32, limit uint32, burst uint32) error {
	ifByName, err := net.InterfaceByName(ifName)
	if err != nil {
		klog.Errorf("cannot find %s interface", ifName)
		return err
	}
	// create rate
	rate := limit

	htbObject := createClassObject(uint32(ifByName.Index), classid, parent, rate, rate+burst)
	// 这里只能在函数内部创建rtnetlink，因为这个函数可能会在{ns.Do()}中执行
	tcnlInNs, err := createTcnl()
	if err != nil {
		klog.Errorf("[addHTBClass]-err creating tcnl, err = %s", err)
		return err
	}
	defer tcnlInNs.Close()

	if err := tcnlInNs.Class().Add(htbObject); err != nil {
		return err
	}

	return nil
}

// AddTCFilterWithDstCidr 相当于调用
// $TC filter add dev $IF1 protocol ip parent {parent} prio {prio} u32 match ip dst {dstCidr} flowid {classId}
func AddTCFilterWithDstCidr(ifName string, parent uint32, dstCidr string, classId uint32, prio uint16) error {
	ifByName, err := net.InterfaceByName(ifName)
	if err != nil {
		klog.Errorf("未找到名为%s的interface", ifName)
		return err
	}

	// 这里只能在函数内部创建rtnetlink，因为这个函数可能会在{ns.Do()}中执行
	tcnlInNs, err := createTcnl()
	if err != nil {
		klog.Errorf("[AddTCFilterWithDstCidr]-err creating tcnl, err = %s", err)
		return err
	}
	defer tcnlInNs.Close()

	ipAddr, cidr, err := net.ParseCIDR(dstCidr)
	if err != nil {
		klog.Errorf("解析CIDR出错, err=%s", err)
		return err
	}
	// 将这个ip转化为uint32
	dstIP := ipAddr.To4()
	if dstIP == nil {
		klog.Errorf("无法将%s转化为IPv4", ipAddr)
		return err
	}
	dstIPUint32 := uint32(dstIP[3])<<24 | uint32(dstIP[2])<<16 | uint32(dstIP[1])<<8 | uint32(dstIP[0])
	// 获取Mask掩码表示
	mask := cidr.Mask
	// 将mask位数转化为, 例如 24 -> 0xffffff00
	maskUint32 := uint32(mask[3])<<24 | uint32(mask[2])<<16 | uint32(mask[1])<<8 | uint32(mask[0])

	tcU32Keys := make([]tc.U32Key, 0)
	tcU32Keys = append(tcU32Keys, tc.U32Key{
		Mask: maskUint32,
		Val:  dstIPUint32,
		Off:  16, // 匹配ip字段
	})
	filterObj := createFilterObject(uint32(ifByName.Index), core.BuildHandle(1, 0), core.BuildHandle(1, 1), prio, ProtocolIP, tcU32Keys...)
	err = tcnlInNs.Filter().Add(filterObj)
	return err
}

func createFilterObject(ifIndex uint32, parent uint32, flowid uint32, prio uint16, protocol Protocol, keys ...tc.U32Key) *tc.Object {
	flag := TCA_CLS_FLAGS_SKIP_HW
	selFlag := TC_U32_TERMINAL
	info := uint32(0)
	info |= (uint32(prio) << 16)
	info |= uint32(protocol)
	return &tc.Object{
		Msg: tc.Msg{
			Family:  unix.AF_UNSPEC,
			Ifindex: uint32(ifIndex),
			Parent:  parent,
			Info:    info,
		},

		Attribute: tc.Attribute{
			Kind: "u32",
			U32: &tc.U32{
				ClassID: &flowid,
				Flags:   &flag,
				Sel: &tc.U32Sel{
					Flags: selFlag,
					Keys:  keys,
					NKeys: uint8(len(keys)),
				},
			},
		},
	}
}

// 可以为空的选项使用指针传递
func createHTBObject(ifIndex uint32, handle uint32, parent uint32, rate *uint64, htbInit *tc.HtbGlob) *tc.Object {
	return &tc.Object{
		Msg: tc.Msg{
			Family:  unix.AF_UNSPEC,
			Ifindex: uint32(ifIndex),
			Handle:  handle,
			Parent:  parent,
			Info:    0,
		},
		Attribute: tc.Attribute{
			Kind: "htb",
			Htb: &tc.Htb{
				Init:   htbInit,
				Rate64: rate,
			},
		},
	}
}

func createClassObject(ifIndex uint32, handle uint32, parent uint32, rate uint32, burst uint32) *tc.Object {
	return &tc.Object{
		Msg: tc.Msg{
			Family:  unix.AF_UNSPEC,
			Ifindex: ifIndex,
			Handle:  handle,
			Parent:  parent,
		},
		Attribute: tc.Attribute{
			Kind: "htb",
			Htb: &tc.Htb{
				Parms: &tc.HtbOpt{
					Rate: tc.RateSpec{
						CellLog:   0x3,
						Linklayer: 0x1,
						Overhead:  0x0,
						CellAlign: 0xffff,
						Mpu:       0x0,
						Rate:      rate,
					},
					Ceil: tc.RateSpec{
						CellLog:   0x3,
						Linklayer: 0x1,
						Overhead:  0x0,
						CellAlign: 0xffff,
						Mpu:       0x0,
						Rate:      rate + burst,
					},
				},
			},
		},
	}
}

func GetFilter(ifName string) ([]tc.Object, error) {
	ifByName, err := net.InterfaceByName(ifName)
	if err != nil {
		klog.Errorf("未找到名为%s的interface", ifName)
		return nil, err
	}

	tcnlInNs, err := createTcnl()
	if err != nil {
		klog.Errorf("[GetFileter]-err creating tcnl, err = %s", err)
		return nil, err
	}
	defer tcnlInNs.Close()

	msg := &tc.Msg{
		Ifindex: uint32(ifByName.Index),
	}
	res, err := tcnlInNs.Filter().Get(msg)
	if err != nil {
		klog.Errorf("[GetFilter]-获取Filter信息失败, err=%s", err)
		return nil, err
	}

	return res, nil
}

func createTcnl() (*tc.Tc, error) {
	tcnl, err := tc.Open(&tc.Config{})
	if err != nil {
		klog.Errorf("[createTcnl]-err creating tcnl, err = %s", err)
		return nil, err
	}
	if err := tcnl.SetOption(netlink.ExtendedAcknowledge, true); err != nil {
		klog.Warningf("[createTcnl]-EXT_ACK set failed, err = %v", err)
	}
	return tcnl, nil
}

// validateBandwithFormat 使用正则表达式验证带宽格式。支持的单位有k, K, m, M, kbps, mbps, Kbps, Mbps
func validateBandwithFormat(bandwidth string) (uint32, error) {

	checkReStr := `^[1-9][0-9]*(k|K|m|M|kbps|mbps|Kbps|Mbps)?$`
	checkRe, err := regexp.Compile(checkReStr)
	if err != nil {
		return 0, errors.New("正则表达式编译失败")
	}
	if matchresult := checkRe.MatchString(bandwidth); !matchresult {
		return 0, errors.New("带宽格式不正确")
	}
	// 提取数字
	numReStr := `^[1-9][0-9]*`
	numRe, _ := regexp.Compile(numReStr)

	numStr := numRe.FindString(bandwidth)
	rateNum, err := strconv.ParseUint(numStr, 10, 32)

	if err != nil {
		return 0, errors.New("解析带宽数字失败")
	}

	// 提取单位
	unitReStr := `(k|K|m|M|kbps|mbps|Kbps|Mbps)?$`
	unitRe, _ := regexp.Compile(unitReStr)
	unitStr := unitRe.FindString(bandwidth)
	switch unitStr {
	case "k", "K", "kbps", "Kbps":
		rateNum = rateNum * 1000
	case "m", "M", "mbps", "Mbps":
		rateNum = rateNum * 1000 * 1000
	}

	return uint32(rateNum), nil
}
