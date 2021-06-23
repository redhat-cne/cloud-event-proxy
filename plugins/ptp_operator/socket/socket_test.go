package socket_test

import (
	"bufio"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	ptp_socket "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/socket"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"net"
	"testing"
	"time"
)


const logLength = 17

var logsData = [logLength]string{
	"ptp4l[3535499.401]: [ens5f1] port 1: delay timeout " + "\n",
	"ptp4l[3535499.402]: [ens5f1] delay   filtered         88   raw         82 " + "\n",
	"ptp4l[3535432.615]: [ens5f1] port 1: UNCALIBRATED to SLAVE on MASTER_CLOCK_SELECTED " + "\n",
	"ptp4l[3535499.424]: [ens5f1] master offset          1 s0 freq   -1869 path delay        88 " + "\n",
	"phc2sys[1095550.462]: [ens5f0] CLOCK_REALTIME phc offset      -233 s2 freq  -77619 delay   1260" + "\n",
	"ptp4l[1095550.490]: [ens5f0] master offset         52 s2 freq   -1535 path delay        91" + "\n",
	"ptp4l[1095550.506]: [ens5f0] port 1: delay timeout" + "\n",
	"ptp4l[1095550.506]: [ens5f0] delay   filtered         91   raw         81" + "\n",
	"ptp4l[1095550.507]: [ens5f0] port 1: delay timeout" + "\n",
	"tp4l[1095550.507]: [ens5f0] delay   filtered         89   raw         83" + "\n",
	"ptp4l[3535499.488]: [ens5f1] master offset         12 s2 freq   -1850 path delay        88 " + "\n",
	"phc2sys[3535433.762]: [ens5f1] reconfiguring after port state change " + "\n",
	"phc2sys[3535433.762]: [ens5f1] selecting CLOCK_REALTIME for synchronization " + "\n",
	"phc2sys[3535433.762]: [ens5f1] selecting ens5f0 as the master " + "\n",
	"phc2sys[96254.969]: [ens5f1] CLOCK_REALTIME phc offset      100 s2 freq  -79243 delay   1058 " + "\n",
	"phc2sys[432313.127]: [ens5f1] CLOCK_REALTIME phc offset   -837364 s2 freq +625227 delay   1415 " + "\n",
	"ptp4l[432313.222]: [ens5f1] port 1: SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED) " + "\n",

}
var eventProcessor *metrics.PTPEventManager
var pubID="123"
var scConfig          *common.SCConfiguration

func setup(){
	scConfig = &common.SCConfiguration{
	}
}


func Test_WriteMetricsToSocket(t *testing.T) {
	setup()
	eventProcessor = metrics.NewPTPEventManager(pubID,"tetsnode",scConfig)
	eventProcessor.MockTest(true)
	go listenToTestMetrics()
	time.Sleep(2 * time.Second)
	c, err := net.Dial("unix", "/tmp/go.sock")
	assert.Nil(t, err)
	if err != nil {
		return
	}

	defer c.Close()

	for i := 0; i < logLength; i++ {
		_, err = c.Write([]byte(logsData[i]))
		if err != nil {
			log.Fatal("write error:", err)
		}
	}

	time.Sleep(3*time.Second)
}

func listenToTestMetrics() {
	l, err := ptp_socket.Listen("/tmp/go.sock")
	if err != nil {
		log.Printf("error setting up socket %s", err)
		return
	} else {
		log.Printf("connection established successfully")
	}

	for {
		fd, err := l.Accept()
		if err != nil {
			log.Printf("accept error: %s", err)
		} else {
			processTestMetrics2(fd)
		}
	}
}

func processTestMetrics2(c net.Conn) {
	// echo received messages
	remoteAddr := c.RemoteAddr().String()
	log.Println("Client connected from", remoteAddr)
	scanner := bufio.NewScanner(c)
	for {
		ok := scanner.Scan()
		if !ok {
			break
		}
		//log.Printf("plugin got %s", scanner.Text())
		msg := scanner.Text()
		eventProcessor.ExtractMetrics(msg)
	}

}

