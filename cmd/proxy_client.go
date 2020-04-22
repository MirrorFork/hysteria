package main

import (
	"crypto/tls"
	"crypto/x509"
	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/congestion"
	hyCongestion "github.com/tobyxdd/hysteria/pkg/congestion"
	"github.com/tobyxdd/hysteria/pkg/core"
	"github.com/tobyxdd/hysteria/pkg/socks5"
	"io/ioutil"
	"log"
	"os/user"
)

func proxyClient(args []string) {
	var config proxyClientConfig
	err := loadConfig(&config, args)
	if err != nil {
		log.Fatalln("Unable to load configuration:", err)
	}
	if err := config.Check(); err != nil {
		log.Fatalln("Configuration error:", err)
	}
	if len(config.Name) == 0 {
		usr, err := user.Current()
		if err == nil {
			config.Name = usr.Name
		}
	}
	log.Printf("Configuration loaded: %+v\n", config)

	tlsConfig := &tls.Config{
		NextProtos: []string{proxyTLSProtocol},
		MinVersion: tls.VersionTLS13,
	}
	// Load CA
	if len(config.CustomCAFile) > 0 {
		bs, err := ioutil.ReadFile(config.CustomCAFile)
		if err != nil {
			log.Fatalln("Unable to load CA file:", err)
		}
		cp := x509.NewCertPool()
		if !cp.AppendCertsFromPEM(bs) {
			log.Fatalln("Unable to parse CA file", config.CustomCAFile)
		}
		tlsConfig.RootCAs = cp
	}

	quicConfig := &quic.Config{
		MaxReceiveStreamFlowControlWindow:     config.ReceiveWindowConn,
		MaxReceiveConnectionFlowControlWindow: config.ReceiveWindow,
		KeepAlive:                             true,
	}
	if quicConfig.MaxReceiveStreamFlowControlWindow == 0 {
		quicConfig.MaxReceiveStreamFlowControlWindow = DefaultMaxReceiveStreamFlowControlWindow
	}
	if quicConfig.MaxReceiveConnectionFlowControlWindow == 0 {
		quicConfig.MaxReceiveConnectionFlowControlWindow = DefaultMaxReceiveConnectionFlowControlWindow
	}

	client, err := core.NewClient(config.ServerAddr, config.Name, "", tlsConfig, quicConfig,
		uint64(config.UpMbps)*mbpsToBps, uint64(config.DownMbps)*mbpsToBps,
		func(refBPS uint64) congestion.SendAlgorithmWithDebugInfos {
			return hyCongestion.NewBrutalSender(congestion.ByteCount(refBPS))
		})
	if err != nil {
		log.Fatalln("Client initialization failed:", err)
	}
	defer client.Close()
	log.Println("Connected to", config.ServerAddr)

	socks5server, err := socks5.NewServer(config.SOCKS5Addr, "", nil, 0, 0, 0)
	if err != nil {
		log.Fatalln("SOCKS5 server initialization failed:", err)
	}
	log.Println("SOCKS5 server up and running on", config.SOCKS5Addr)

	log.Fatalln(socks5server.ListenAndServe(&socks5.HyHandler{
		Client: client,
		NewTCPRequestFunc: func(addr, reqAddr string) {
			log.Printf("[TCP] %s <-> %s\n", addr, reqAddr)
		},
		TCPRequestClosedFunc: func(addr, reqAddr string, err error) {
			log.Printf("Closed [TCP] %s <-> %s: %s\n", addr, reqAddr, err.Error())
		},
	}))
}