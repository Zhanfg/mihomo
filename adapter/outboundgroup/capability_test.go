package outboundgroup

import (
	"testing"

	"github.com/metacubex/mihomo/common/structure"
)

func TestRequireCapabilityDecode(t *testing.T) {
	decoder := structure.NewDecoder(structure.Option{TagName: "group", WeaklyTypedInput: true})
	raw := map[string]any{
		"name":        "自动选择",
		"type":        "url-test",
		"prefer-udp":  true,
		"prefer-ipv6": true,
		"use":         []any{"SSLinks"},
	}
	opt := GroupCommonOption{}
	if err := decoder.Decode(raw, &opt); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !opt.PreferUDP {
		t.Error("prefer-udp 未解析为 true")
	}
	if !opt.PreferIPv6 {
		t.Error("prefer-ipv6 未解析为 true")
	}
	t.Logf("decoded: PreferUDP=%v PreferIPv6=%v", opt.PreferUDP, opt.PreferIPv6)
}


func TestCustomIPFamilyDirectivesDecode(t *testing.T) {
	decoder := structure.NewDecoder(structure.Option{TagName: "group", WeaklyTypedInput: true})

	smartRaw := map[string]any{
		"name":           "双栈智能",
		"type":           "smart",
		"prefer-ipv4":    true,
		"prefer-ipv6":    true,
		"require-ipv4":   false,
		"require-ipv6":   true,
		"auto-ip-family": true,
	}
	smartOpt := SmartOption{}
	if err := decoder.Decode(smartRaw, &smartOpt); err != nil {
		t.Fatalf("decode smart directives: %v", err)
	}
	if !smartOpt.PreferIPv4 || !smartOpt.RequireIPv6 || !smartOpt.AutoIPFamily {
		t.Fatalf("unexpected smart directives: %+v", smartOpt)
	}

	urlRaw := map[string]any{
		"name":         "IPv6节点",
		"type":         "url-test",
		"prefer-ipv4":  false,
		"require-ipv4": false,
		"require-ipv6": true,
	}
	urlOpt := URLTestOption{}
	if err := decoder.Decode(urlRaw, &urlOpt); err != nil {
		t.Fatalf("decode url-test directives: %v", err)
	}
	if !urlOpt.RequireIPv6 || urlOpt.RequireIPv4 || urlOpt.PreferIPv4 {
		t.Fatalf("unexpected url-test directives: %+v", urlOpt)
	}
}
