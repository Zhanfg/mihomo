package lightgbm

import (
	"bytes"
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/mihomo/component/smart"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"
)

var (
	collectMutex   sync.Mutex
	smartCollector *DataCollector
)

type DataCollector struct {
	mutex              sync.Mutex
	sampleCount        int
	dataPath           string
	file               *os.File
	writer             *csv.Writer
	configured         bool
	smartCollectorSize int64
	lastFileCheck      time.Time
	currentSize        int64
}

const (
	defaultSmartCollectorSize    = 64 * 1024 * 1024
	maxSmartCollectorSize        = 128 * 1024 * 1024
	maxSmartCollectorSizeAndroid = 64 * 1024 * 1024
	expectedColumns              = MaxFeatureSize + 11
)

func normalizeCollectorSize(requested int64, android bool) int64 {
	maxSize := int64(maxSmartCollectorSize)
	if android {
		maxSize = maxSmartCollectorSizeAndroid
	}
	if requested <= 0 {
		requested = defaultSmartCollectorSize
	}
	if requested > maxSize {
		return maxSize
	}
	return requested
}

func removeLegacyCollectorBackups(path string) {
	matches, err := filepath.Glob(path + ".bak.*")
	if err != nil {
		return
	}
	for _, match := range matches {
		_ = os.Remove(match)
	}
}

func InitCollector(collectSize float64) {
	var requested int64
	if collectSize > 0 {
		requested = int64(collectSize * 1024 * 1024)
	}
	smartCollectorSize := normalizeCollectorSize(requested, runtime.GOOS == "android")

	collectMutex.Lock()
	defer collectMutex.Unlock()

	dataPath := filepath.Join(C.Path.HomeDir(), "smart_weight_data.csv")
	if smartCollector == nil {
		smartCollector = &DataCollector{
			dataPath:           dataPath,
			smartCollectorSize: smartCollectorSize,
		}
		return
	}
	if err := smartCollector.reconfigure(dataPath, smartCollectorSize); err != nil {
		log.Warnln("[Smart] Failed to reconfigure data collector: %v", err)
	}
}

func GetCollector() *DataCollector {
	collectMutex.Lock()
	defer collectMutex.Unlock()
	return smartCollector
}

func (c *DataCollector) AddSample(input *smart.ModelInput, metadata *C.Metadata, actualWeight, teacherWeight float64, weightSource string) {
	if c == nil {
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.configured && time.Since(c.lastFileCheck) > 5*time.Second {
		c.lastFileCheck = time.Now()
		if _, err := os.Stat(c.dataPath); os.IsNotExist(err) {
			log.Infoln("[Smart] Data file was deleted, reinitializing collector")
			c.configured = false
			if c.file != nil {
				c.file.Close()
				c.file = nil
			}
			c.writer = nil
		}
	}

	// currentSize tracks logical CSV bytes, including rows still buffered by
	// csv.Writer. This prevents buffered writes from slipping past the hard
	// collector budget.
	if c.currentSize >= c.smartCollectorSize {
		log.Infoln("[Smart] Maximum file size limit reached (%d MB), stopping data collection", c.smartCollectorSize/(1024*1024))
		return
	}

	if !c.configured {
		err := c.initializeWriter()
		if err != nil {
			log.Warnln("[Smart] Failed to initialize training data collector: %v", err)
			return
		}
	}

	features := prepareFeatures(input)
	if len(features) == 0 {
		log.Debugln("[Smart] Feature extraction failed, skipping sample collection")
		return
	}

	featureStrings := make([]string, len(features))
	var buf [32]byte
	for i, f := range features {
		featureStrings[i] = string(strconv.AppendFloat(buf[:0], f, 'f', 6, 64))
	}

	var geoIPStr string
	if metadata.DstGeoIP != nil {
		geoIPStr = strings.Join(metadata.DstGeoIP, ",")
	} else {
		geoIPStr = "unknown"
	}

	var dstASN string
	if metadata.DstIPASN != "" {
		dstASN = metadata.DstIPASN
	} else {
		dstASN = "unknown"
	}

	dstIP := "unknown"
	if metadata.DstIP.IsValid() {
		dstIP = metadata.DstIP.String()
	}

	host := "unknown"
	if metadata.Host != "" {
		host = metadata.Host
	}

	standardizedSource := weightSource
	if standardizedSource == "" {
		standardizedSource = "unknown"
	}

	sample := make([]string, 0, expectedColumns)
	sample = append(sample, featureStrings...)
	sample = append(sample,
		input.GroupName,
		input.NodeName,
		dstASN,
		host,
		dstIP,
		strconv.FormatUint(uint64(metadata.DstPort), 10),
		geoIPStr,
		strconv.FormatFloat(actualWeight, 'f', 6, 64),
		strconv.FormatFloat(teacherWeight, 'f', 6, 64),
		standardizedSource,
		time.Now().Format(time.RFC3339),
	)

	if len(sample) != expectedColumns {
		return
	}

	var encoded bytes.Buffer
	sizeWriter := csv.NewWriter(&encoded)
	if err := sizeWriter.Write(sample); err != nil {
		return
	}
	sizeWriter.Flush()
	if err := sizeWriter.Error(); err != nil {
		return
	}
	rowSize := int64(encoded.Len())
	if rowSize <= 0 || c.currentSize+rowSize > c.smartCollectorSize {
		log.Infoln("[Smart] Collector byte budget exhausted; dropping further training rows")
		return
	}

	if err := c.writer.Write(sample); err != nil {
		log.Warnln("[Smart] Failed to write training data: %v", err)
		c.configured = false
		if c.file != nil {
			c.file.Close()
			c.file = nil
		}
		c.writer = nil
		return
	}
	c.currentSize += rowSize

	c.sampleCount++

	// 每100条记录刷新一次
	if c.sampleCount%100 == 0 {
		c.writer.Flush()
	}
}

func (c *DataCollector) initializeWriter() error {
	var err error

	log.Infoln("[Smart] Initializing data collector for %s", c.dataPath)

	fileExists := false
	if _, err := os.Stat(c.dataPath); err == nil {
		fileExists = true
	}

	needUpgrade := false
	if fileExists {
		f, err := os.Open(c.dataPath)
		if err == nil {
			defer f.Close()
			reader := csv.NewReader(f)
			headers, err := reader.Read()
			if err == nil {
				hasLoss := false
				hasTeacher := false
				for _, h := range headers {
					switch h {
					case "cumul_loss_rate":
						hasLoss = true
					case "teacher_weight":
						hasTeacher = true
					}
				}
				if !hasLoss || !hasTeacher {
					needUpgrade = true
				}
			}
		}
	}

	if needUpgrade {
		// Keep exactly one migration backup. Historical timestamped backups from
		// older builds are removed so schema churn cannot grow storage without
		// bound over a long-lived installation.
		removeLegacyCollectorBackups(c.dataPath)
		backupPath := c.dataPath + ".bak"
		_ = os.Remove(backupPath)
		if stat, statErr := os.Stat(c.dataPath); statErr == nil && stat.Size() <= c.smartCollectorSize {
			if err := os.Rename(c.dataPath, backupPath); err != nil {
				return err
			}
			log.Infoln("[Smart] Old CSV schema backed up to %s", backupPath)
		} else {
			_ = os.Remove(c.dataPath)
			log.Warnln("[Smart] Old CSV schema exceeded collector budget and was discarded")
		}
		fileExists = false
	}

	file, err := os.OpenFile(c.dataPath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}

	if fileExists {
		if stat, err2 := file.Stat(); err2 == nil && stat.Size() > 0 {
			last := make([]byte, 1)
			if _, err2 = file.ReadAt(last, stat.Size()-1); err2 == nil && last[0] != '\n' {
				const scanSize = int64(65536)
				readStart := stat.Size() - scanSize
				if readStart < 0 {
					readStart = 0
				}
				buf := make([]byte, stat.Size()-readStart)
				if _, err3 := file.ReadAt(buf, readStart); err3 == nil {
					newlinePos := int64(-1)
					for i := int64(len(buf)) - 1; i >= 0; i-- {
						if buf[i] == '\n' {
							newlinePos = readStart + i + 1
							break
						}
					}
					if newlinePos > 0 {
						_ = file.Truncate(newlinePos)
						log.Warnln("[Smart] Removed incomplete CSV row from %s", c.dataPath)
					} else {
						_ = file.Truncate(0)
						fileExists = false
						log.Warnln("[Smart] No valid CSV rows found, reinitializing %s", c.dataPath)
					}
				}
			}
		}
	}

	c.file = file
	c.writer = csv.NewWriter(c.file)

	if !fileExists {
		headers := []string{
			"success", "failure", "connect_time", "latency",
			"upload_mb", "history_upload_mb", "maxuploadrate_kb", "history_maxuploadrate_kb",
			"download_mb", "history_download_mb", "maxdownloadrate_kb", "history_maxdownloadrate_kb",
			"duration_minutes", "history_duration_minutes", "last_used_seconds",
			"is_udp", "is_tcp",
			"loss_rate", "cumul_loss_rate",
			"asn_feature",
			"country_feature",
			"address_feature",
			"port_feature",
			"traffic_ratio", "traffic_density", "connection_type_feature",
			"asn_hash", "host_hash", "ip_hash", "geoip_hash",
			"group_name", "node_name",
			"asn_raw", "host_raw", "ip_raw", "port_raw", "geoip_raw",
			"weight", "teacher_weight", "weight_source", "timestamp",
		}

		if err := c.writer.Write(headers); err != nil {
			c.file.Close()
			return err
		}
		c.writer.Flush()
	}

	if c.writer != nil {
		c.writer.Flush()
		if err := c.writer.Error(); err != nil {
			_ = c.file.Close()
			c.file = nil
			c.writer = nil
			return err
		}
	}
	if stat, err := c.file.Stat(); err == nil {
		c.currentSize = stat.Size()
	} else {
		c.currentSize = 0
	}
	c.configured = true
	return nil
}

func (c *DataCollector) Flush() error {
	if c == nil {
		return nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.writer != nil {
		c.writer.Flush()
	}

	return nil
}

func (c *DataCollector) Close() error {
	if c == nil {
		return nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.closeLocked()
}

func (c *DataCollector) closeLocked() error {
	var result error
	if c.writer != nil {
		c.writer.Flush()
		result = c.writer.Error()
	}

	if c.file != nil {
		result = errors.Join(result, c.file.Close())
	}
	c.file = nil
	c.writer = nil
	c.currentSize = 0
	c.configured = false
	return result
}

func (c *DataCollector) reconfigure(dataPath string, collectorSize int64) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var err error
	if c.dataPath != dataPath {
		err = c.closeLocked()
		c.dataPath = dataPath
	}
	c.smartCollectorSize = normalizeCollectorSize(collectorSize, runtime.GOOS == "android")
	return err
}

func CloseAllCollectors() {
	collectMutex.Lock()
	defer collectMutex.Unlock()

	if smartCollector != nil {
		smartCollector.Close()
	}
}
