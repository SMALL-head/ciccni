package iptables

import (
	"fmt"

	"github.com/coreos/go-iptables/iptables"
	"k8s.io/klog"
)

const (
	NATTable    = "nat"
	FilterTable = "filter"

	AcceptTarget     = "ACCEPT"
	MarkTarget       = "MARK"
	MasqueradeTarget = "MASQUERADE"

	ForwardChain           = "FORWARD"
	CICCNIForwardChain     = "CICCNI-FORWARD"
	CICCNIPostRoutingChain = "CICCNI-POSTROUTING"
	PostRoutingChain       = "POSTROUTING"
)

const (
	ExternalPkgMark = "0x40/0x40"
)

type Client struct {
	ipt         *iptables.IPTables
	hostGateway string
	podCIDR     string
}

func NewClient(hostGateway string, podCIDR string) (*Client, error) {
	ipt, err := iptables.New()
	if err != nil {
		return nil, fmt.Errorf("error creating IPTables instance: %v", err)
	}
	return &Client{
		ipt:         ipt,
		hostGateway: hostGateway,
		podCIDR:     podCIDR,
	}, nil
}

// rule is a generic struct that describes an iptables rule.
type rule struct {
	// The table of this rule. eg: nat, filter...
	table string
	// The chain of this rule. eg: PREROUTING, INPUT...
	chain string
	// The parameters that make up a rule specification, eg: '-i ifaceName', '-o portName', '-p tcp'...
	parameters []string
	// The target of this rule. eg: ACCEPT, DROP...
	target        string
	targetOptions []string
	comment       string
}

// SetUpRules 在主机上安装多条预置的iptables规则
func (c *Client) SetUpRules(outInterface string) error {
	rules := []rule{
		// iptables -t filter -A FORWARD -j {CICCNIForwardChain}
		{FilterTable, ForwardChain, nil, CICCNIForwardChain, nil, "ciccni: 跳转至CICCNI-FORWARD链"},

		// iptables -t filter -A {CICCNIForwardChain} -m comment --comment '标记位0x40/0x40' -i {gw名} ! -o {gw名} -j MARK --set-xmark 0x40/0x40
		{FilterTable, CICCNIForwardChain, []string{"-i", c.hostGateway, "!", "-o", c.hostGateway}, MarkTarget, []string{"--set-xmark", ExternalPkgMark}, "ciccni: 标记位0x40/0x40"},

		// iptables -A {CICCNIForwardChain} -m comment --comment '接收外部包' -i gw0 ! -o gw0 -j ACCEPT
		{FilterTable, CICCNIForwardChain, []string{"-i", c.hostGateway, "!", "-o", c.hostGateway}, AcceptTarget, nil, "ciccni: 接收pod to External包"},
		{FilterTable, CICCNIForwardChain, []string{"!", "-i", c.hostGateway, "-o", c.hostGateway}, AcceptTarget, nil, "ciccni: 接收external to pod traffic"},

		// iptables -t nat -A POSTROUTING -j {CICCNIPostRoutingChain} -m comment --comment '跳转{CICCNIPostRoutingChain}链'
		{NATTable, PostRoutingChain, nil, CICCNIPostRoutingChain, nil, "ciccni: 跳转至CICCNI-POSTROUTING链"},

		// iptables -t nat -A {CICCNIPostRoutingChain} -m mark --mark 0x40/0x40 -j MASQUERADE -m comment --comment 'SNAT'
		{NATTable, CICCNIPostRoutingChain, []string{"-m", "mark", "--mark", ExternalPkgMark}, MasqueradeTarget, nil, "ciccni: for host gateway"},

		// iptables -t filter -A {CICCNIForwardChain} -m comment --comment 'ciccni: 默认接受' -j ACCEPT
		{FilterTable, CICCNIForwardChain, nil, AcceptTarget, nil, "ciccni: 默认接受"},
	}

	// Ensure all the chains involved exist.
	for _, rule := range rules {
		if err := c.ensureChain(rule.table, rule.chain); err != nil {
			return err
		}
	}

	// Ensure all the rules exist.
	for _, rule := range rules {
		var ruleSpec []string
		ruleSpec = append(ruleSpec, rule.parameters...)
		ruleSpec = append(ruleSpec, "-j", rule.target)
		ruleSpec = append(ruleSpec, rule.targetOptions...)
		ruleSpec = append(ruleSpec, "-m", "comment", "--comment", rule.comment)
		if err := c.ensureRule(rule.table, rule.chain, ruleSpec); err != nil {
			return err
		}
	}
	return nil
}

// ensureChain checks if target chain already exists, creates it if not.
func (c *Client) ensureChain(table string, chain string) error {
	oriChains, err := c.ipt.ListChains(table)
	if err != nil {
		return fmt.Errorf("error listing exsiting chains in table %s: %v", table, err)
	}
	if contains(oriChains, chain) {
		return nil
	}
	if err := c.ipt.NewChain(table, chain); err != nil {
		return fmt.Errorf("error creating chain %s in table %s: %v", chain, table, err)
	}
	klog.V(2).Infof("Created chain %s in table %s", chain, table)
	return nil
}

// ensureRule checks if target rule already exists, appends it if not.
func (c *Client) ensureRule(table string, chain string, ruleSpec []string) error {
	exist, err := c.ipt.Exists(table, chain, ruleSpec...)
	if err != nil {
		return fmt.Errorf("error checking if rule %v exists in table %s chain %s: %v", ruleSpec, table, chain, err)
	}
	if exist {
		return nil
	}
	if err := c.ipt.Append(table, chain, ruleSpec...); err != nil {
		return fmt.Errorf("error appending rule %v to table %s chain %s: %v", ruleSpec, table, chain, err)
	}
	klog.V(2).Infof("Appended rule %v to table %s chain %s", ruleSpec, table, chain)
	return nil
}

func contains(chains []string, targetChain string) bool {
	for _, val := range chains {
		if val == targetChain {
			return true
		}
	}
	return false
}
