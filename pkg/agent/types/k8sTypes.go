package types

type ClusterConfig struct {
	Networking struct {
        PodSubnet string `yaml:"podSubnet"`
    } `yaml:"networking"`
}