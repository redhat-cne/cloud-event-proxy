package ptp4lconf_test

import (
	"fmt"

	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"testing"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
)

const (
	dirToWatch = "../_testFiles/"
	ptp4l0Conf = "ptp4l.0.config"
)

var (
	initialText = fmt.Sprintf("%d", time.Now().UnixNano())
)

func SetUp() error {
	cleanUp()
	f, err := os.OpenFile(filepath.Join(dirToWatch, ptp4l0Conf), os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Errorf("create file error %s", err)
		return err
	}
	defer f.Close()
	_, err = f.Write([]byte(initialText))
	if err != nil {
		return err
	}
	return nil
}
func cleanUp() {
	err := os.Remove(filepath.Join(dirToWatch, ptp4l0Conf))
	if err != nil {
		log.Println(err)
		return
	}
}
func Test_Config(t *testing.T) {
	var err error
	defer cleanUp()
	err = SetUp()
	assert.Nil(t, err)

	notifyConfigUpdates := make(chan *ptp4lconf.PtpConfigUpdate)
	w, err := ptp4lconf.NewPtp4lConfigWatcher(dirToWatch, notifyConfigUpdates)
	assert.Nil(t, err)
	select {
	case ptpConfigEvent := <-notifyConfigUpdates:
		//assert
		assert.Equal(t, ptp4l0Conf, *ptpConfigEvent.Name)
		assert.Equal(t, initialText, *ptpConfigEvent.Ptp4lConf)
	case <-time.After(1 * time.Second):
		log.Infof("timeout...")
	}

	//Update config
	newText := fmt.Sprintf("%d", time.Now().UnixNano())
	log.Infof("writing to %s", filepath.Join(dirToWatch, ptp4l0Conf))
	err = ioutil.WriteFile(filepath.Join(dirToWatch, ptp4l0Conf), []byte(newText), 0644)
	assert.Nil(t, err)
	log.Info("waiting...")
	// WriteFile creates two events it might be an issue with test only
	select {
	case <-notifyConfigUpdates:
		//assert
	case <-time.After(1 * time.Second):
		log.Infof("timeout...")
	}

	select {
	case ptpConfigEvent := <-notifyConfigUpdates:
		//assert
		assert.Equal(t, ptp4l0Conf, *ptpConfigEvent.Name)
		assert.Equal(t, newText, *ptpConfigEvent.Ptp4lConf)
	case <-time.After(1 * time.Second):
		log.Infof("timeout...")
	}

	cleanUp()
	select {
	case ptpConfigEvent := <-notifyConfigUpdates:
		//assert
		log.Println(ptpConfigEvent.String())
		assert.Nil(t, err)
		assert.Equal(t, ptp4l0Conf, *ptpConfigEvent.Name)
		assert.True(t, ptpConfigEvent.Removed)
	case <-time.After(1 * time.Second):
		log.Infof("timeout...")
	}

	w.Close()

}
