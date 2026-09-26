package mmdb

import (
	"net"
	"sync"

	mihomoOnce "github.com/metacubex/mihomo/common/once"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"

	"github.com/oschwald/maxminddb-golang"
)

type databaseType = uint8

const (
	typeMaxmind databaseType = iota
	typeSing
	typeMetaV0
)

var (
	ipReader  IPReader
	asnReader ASNReader
	ipOnce    sync.Once
	asnOnce   sync.Once
)

func LoadFromBytes(buffer []byte) {
	ipOnce.Do(func() {
		mmdb, err := maxminddb.FromBytes(buffer)
		if err != nil {
			log.Fatalln("Can't load mmdb: %s", err.Error())
		}
		ipReader = IPReader{Reader: mmdb}
		switch mmdb.Metadata.DatabaseType {
		case "sing-geoip":
			ipReader.databaseType = typeSing
		case "Meta-geoip0":
			ipReader.databaseType = typeMetaV0
		default:
			ipReader.databaseType = typeMaxmind
		}
	})
}

func Verify(path string) bool {
	instance, err := maxminddb.Open(path)
	if err == nil {
		instance.Close()
	}
	return err == nil
}

// LookupCodeOptional performs a one-shot country lookup without touching the
// process-global MMDB singleton.
//
// Optional Smart telemetry must never be able to terminate the data plane.
// IPInstance intentionally preserves Mihomo's historical fatal-on-missing
// behaviour for GEOIP rules; country-affinity uses this non-fatal helper
// instead. Country results are cached by the caller, so this path is not on
// every packet/connection hot path.
func LookupCodeOptional(path string, ip net.IP) ([]string, error) {
	reader, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	r := IPReader{Reader: reader}
	switch reader.Metadata.DatabaseType {
	case "sing-geoip":
		r.databaseType = typeSing
	case "Meta-geoip0":
		r.databaseType = typeMetaV0
	default:
		r.databaseType = typeMaxmind
	}
	return r.LookupCode(ip), nil
}


func LookupASNOptional(path string, ip net.IP) (asn string, aso string, err error) {
	reader, err := maxminddb.Open(path)
	if err != nil {
		return "", "", err
	}
	defer reader.Close()
	r := ASNReader{Reader: reader}
	asn, aso = r.LookupASN(ip)
	return asn, aso, nil
}

func IPInstance() IPReader {
	ipOnce.Do(func() {
		mmdbPath := C.Path.MMDB()
		log.Infoln("Load MMDB file: %s", mmdbPath)
		mmdb, err := maxminddb.Open(mmdbPath)
		if err != nil {
			log.Errorln("Can't load MMDB; GeoIP lookup is temporarily disabled: %s", err.Error())
			ipReader = IPReader{}
			return
		}
		ipReader = IPReader{Reader: mmdb}
		switch mmdb.Metadata.DatabaseType {
		case "sing-geoip":
			ipReader.databaseType = typeSing
		case "Meta-geoip0":
			ipReader.databaseType = typeMetaV0
		default:
			ipReader.databaseType = typeMaxmind
		}
	})

	return ipReader
}

func ASNInstance() ASNReader {
	asnOnce.Do(func() {
		ASNPath := C.Path.ASN()
		log.Infoln("Load ASN file: %s", ASNPath)
		asn, err := maxminddb.Open(ASNPath)
		if err != nil {
			log.Errorln("Can't load ASN; ASN lookup is temporarily disabled: %s", err.Error())
			asnReader = ASNReader{}
			return
		}
		asnReader = ASNReader{Reader: asn}
	})

	return asnReader
}

func ReloadIP() {
	mihomoOnce.Reset(&ipOnce)
}

func ReloadASN() {
	mihomoOnce.Reset(&asnOnce)
}
