package config

type EBPF struct {
	Enable              bool   `yaml:"enable" json:"enable"`
	Interface           string `yaml:"interface-name" json:"interface-name,omitempty"`
	AutoDetectInterface bool   `yaml:"auto-detect-interface" json:"auto-detect-interface"`
	BypassPrivate       bool   `yaml:"bypass-private-address" json:"bypass-private-address"`
	IPv6                bool   `yaml:"ipv6" json:"ipv6"`
	TCPriority          uint16 `yaml:"tc-priority" json:"tc-priority,omitempty"`
}

func (e EBPF) Equal(other EBPF) bool {
	return e == other
}
