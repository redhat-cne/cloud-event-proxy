package metrics

import (
	"fmt"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

// SyncELogData ... collect synce log data
type SyncELogData struct {
	Process           string
	Timestamp         int64
	Config            string
	Interface         string
	Device            string
	EECState          string
	ClockQuality      string
	ExtQL             byte
	QL                byte
	NetworkOption     int
	State             string
	ExtendedQlEnabled bool
}

// Parse ... synce logs
func (s *SyncELogData) Parse(out []string) error {
	// 0        1             2             3        4     5       6             7             8      9  10
	// synce4l 1722456110 synce4l.0.config ens7f0 device synce1 eec_state EEC_HOLDOVER network_option 2 s1
	// 0        1             2             3           4         5     6     7      8      9        10       11 12 13   14
	// synce4l 1722458091 synce4l.0.config ens7f0 clock_quality PRTC device synce1 ext_ql 0x20 network_option 2 ql 0x1 s2

	if len(out) < 10 {
		return fmt.Errorf("invalid log format: not enough fields")
	}
	timestamp, err := strconv.ParseInt(out[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %v", err)
	}
	s.Process = out[0]
	s.Timestamp = timestamp
	s.Config = out[2]
	s.Interface = out[3]
	s.NetworkOption = 0
	s.State = out[len(out)-1]
	s.ExtendedQlEnabled = false

	for i := 4; i < len(out)-2; i += 2 {
		switch out[i] {
		case "eec_state":
			s.EECState = out[i+1]
		case "clock_quality":
			s.ClockQuality = out[i+1]
		case "ext_ql":
			hexString := strings.TrimPrefix(out[i+1], "0x")
			extQLValue, err2 := strconv.ParseUint(hexString, 16, 8) // Parse as 8-bit unsigned int
			if err2 == nil {
				s.ExtQL = byte(extQLValue)
			}
			s.ExtendedQlEnabled = true
		case "ql":
			hexString := strings.TrimPrefix(out[i+1], "0x")
			qLValue, err2 := strconv.ParseUint(hexString, 16, 8) // Parse as 8-bit unsigned int
			if err2 == nil {
				s.QL = byte(qLValue)
			}
		case "device":
			s.Device = out[i+1]
		case "network_option":
			networkOption, er := strconv.Atoi(out[i+1])
			if er != nil {
				log.Errorf("invalid network option: %v", err)
			}
			s.NetworkOption = networkOption
		}
	}
	return nil
}
