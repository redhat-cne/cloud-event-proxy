package main

import (
	"bufio"
	"github.com/stretchr/testify/assert"
	"log"
	"net"
	"testing"
	"time"
)



func Test_WriteMetricsToSocket(t *testing.T) {
	go listenToTestMetrics()
	time.Sleep(2*time.Second)
	c, err := net.Dial("unix", "/tmp/go.sock")
	assert.Nil(t, err)
	if err!=nil{
		return
	}

	defer c.Close()
	//_, err = c.Write([]byte("{\"name\":\"name\",\"iface\":\"en01\",\"output\":\"test \"}" + "\n"))
	_, err = c.Write([]byte("name::en01::test" + "\n"))
	if err != nil {
		log.Fatal("write error:", err)
	}
	_, err = c.Write([]byte("name::en01::test2" + "\n"))
	if err != nil {
		log.Fatal("write error:", err)
	}
	time.Sleep(2*time.Second)
}

func listenToTestMetrics(){
	l,err:=listen("/tmp/go.sock")
	if err!=nil {
		log.Printf("error setting up socket %s",err)
		return
	}else{
		log.Printf("connection established successfully")
	}
	for {
		fd, err := l.Accept()
		if err != nil {
			log.Printf("accept error: %s", err)
		}else{
			go processTestMetrics2(fd)
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
		log.Printf("plugin got %s",scanner.Text())
	}
}

func processTestMetrics(c net.Conn) {
	for {
		buf := make([]byte, 512)
		// read a single byte which contains the message length
		nr, err := c.Read(buf)
		if err != nil {
			return
		}
		data := buf[0:nr]
		log.Printf("plugin got: %s", string(data))
	}
}