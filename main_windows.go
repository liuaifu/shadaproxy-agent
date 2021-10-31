package main

import (
	"encoding/xml"
	"flag"
	"github.com/liuaifu/winservice"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type ServiceCfg struct {
	Name        string `xml:"name"`
	Key         string `xml:"key"`
	ServiceAddr string `xml:"service_addr"`
	PoolSize    int    `xml:"pool_size"`
}

type Config struct {
	XMLName    xml.Name     `xml:"config"`
	ServerAddr string       `xml:"server_addr"`
	Services   []ServiceCfg `xml:"services>service"`
}

var g_config_file string
var g_config *Config
var g_chClose chan int // 这个channel关闭时，所有routine应退出

func init() {
	execPath, err := exec.LookPath(os.Args[0])
	if err != nil {
		log.Fatal(err)
	}
	//Is Symlink
	fi, err := os.Lstat(execPath)
	if err != nil {
		log.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
		execPath, err = os.Readlink(execPath)
		if err != nil {
			log.Fatal(err)
		}
	}
	execDir := filepath.Dir(execPath)
	if execDir == "." {
		execDir, err = os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
	}
	os.Chdir(execDir)
	flag.StringVar(&g_config_file, "config", filepath.Join(execDir, "config.xml"), "config file")

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds /* | log.Lshortfile*/)
	g_chClose = make(chan int)
}

func isQuitting(timeout time.Duration) bool {
	select {
	case <- time.After(timeout):
		return false
	case <- g_chClose:
		return true
	}
}

func main() {
	println("shadaproxy-agent v0.5")
	println("copyright(c) 2011-2021 laf163@gmail.com")
	println("")
	if len(os.Args) >= 2 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		println("Usage:")
		flag.PrintDefaults()
		return
	}
	flag.Parse()

	logFile, err := os.OpenFile("./shadaproxy-agent.log", os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	defer logFile.Close()
	if err == nil {
		log.SetOutput(logFile)
	} else {
		log.Println(err)
	}
	log.Println("---------------------------------------------------------------")

	data, err := ioutil.ReadFile(g_config_file)
	if err != nil {
		log.Panic(err)
	}
	g_config = &Config{}
	err = xml.Unmarshal(data, g_config)
	if err != nil {
		log.Panic(err)
	}

	winservice.OnServiceStopped = func() {
		log.Printf("service closed\n")
		close(g_chClose)
	}
	winservice.Start("shadaproxy-agent")

	waiter := &sync.WaitGroup{}
	waiter.Add(len(g_config.Services))
	for _, serviceCfg := range g_config.Services {
		log.Printf("service: %s, %s\n", serviceCfg.Name, serviceCfg.ServiceAddr)

		service := newService()
		service.name = serviceCfg.Name
		service.key = serviceCfg.Key
		service.serverAddr = g_config.ServerAddr
		service.serviceAddr = serviceCfg.ServiceAddr
		service.poolSize = serviceCfg.PoolSize
		go func() {
			service.loop()
			waiter.Done()
		}()
	}

	waiter.Wait()
	winservice.Stop()
	log.Printf("finished.")
}
