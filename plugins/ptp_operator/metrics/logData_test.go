package metrics_test

import (
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
)

func TestSyncELogData_Parse(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    *metrics.SyncELogData
		wantErr bool
	}{
		{
			name:  "valid log with eec_state",
			input: []string{"synce4l", "1722456110", "synce4l.0.config", "ens7f0", "device", "synce1", "eec_state", "EEC_HOLDOVER", "network_option", "2", "s1"},
			want: &metrics.SyncELogData{
				Process:       "synce4l",
				Timestamp:     1722456110,
				Config:        "synce4l.0.config",
				Interface:     "ens7f0",
				Device:        "synce1",
				EECState:      "EEC_HOLDOVER",
				ClockQuality:  "",
				NetworkOption: 2,
				State:         "s1",
			},
			wantErr: false,
		},
		{
			name:  "valid log with clock_quality and ext_ql",
			input: []string{"synce4l", "1722458091", "synce4l.0.config", "ens7f0", "clock_quality", "PRTC", "device", "synce1", "ext_ql", "0x20", "network_option", "2", "ql", "0x1", "s2"},
			want: &metrics.SyncELogData{
				Process:           "synce4l",
				Timestamp:         1722458091,
				Config:            "synce4l.0.config",
				Interface:         "ens7f0",
				Device:            "synce1",
				EECState:          "",
				ClockQuality:      "PRTC",
				ExtQL:             0x20,
				QL:                0x1,
				NetworkOption:     2,
				State:             "s2",
				ExtendedQlEnabled: true,
			},
			wantErr: false,
		},
		{
			name:    "invalid log with not enough fields",
			input:   []string{"synce4l", "1722456110", "synce4l.0.config", "ens7f0"},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid log with incorrect timestamp",
			input:   []string{"synce4l", "invalid_timestamp", "synce4l.0.config", "ens7f0", "device", "synce1", "eec_state", "EEC_HOLDOVER", "network_option", "2", "s1"},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &metrics.SyncELogData{}
			err := s.Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SyncELogData.Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && *s != *tt.want {
				t.Errorf("SyncELogData.Parse() = %+v, want %+v", s, tt.want)
			}
		})
	}
}
