package broker

import (
	"crypto/tls"
	log "github.com/funkygao/log4go"
	"github.com/funkygao/mhub/config"
	"net"
)

// A Server holds all the state associated with an MQTT server.
type Server struct {
	cf *config.Config

	stats *stats
	subs  *subscriptions
	peers *peers

	Done chan struct{}
}

// NewServer creates a new MQTT server, which accepts connections from
// the given listener.
func NewServer(cf *config.Config) (this *Server) {
	stats := &stats{interval: cf.Broker.StatsInterval}
	this = &Server{
		cf:    cf,
		stats: stats,
		subs:  newSubscriptions(cf.Broker.SubscriptionsWorkers, stats),
		Done:  make(chan struct{}),
	}
	this.peers = newPeers(this)

	go stats.start()

	return
}

// Start makes the Server start accepting and handling connections.
func (this *Server) Start() {
	listener, err := this.startListener()
	if err != nil {
		panic(err)
	}

	if err := this.peers.start(this.cf.Peers.ListenAddr); err != nil {
		panic(err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Error(err)
				continue
			}

			log.Debug("new client conn %s", conn.RemoteAddr().String())
			this.stats.clientConnect()

			client := &incomingConn{
				server: this,
				conn:   conn,
				jobs:   make(chan job, sendingQueueLength),
			}
			go client.inboundLoop()
			go client.outboundLoop()
		}
	}()
}

func (this *Server) Stop() {
	close(this.Done)
}

func (this *Server) startListener() (listener net.Listener, err error) {
	if this.cf.Broker.ListenAddr != "" {
		listener, err = net.Listen("tcp", this.cf.Broker.ListenAddr)
		log.Info("Accepting client conn on %s", this.cf.Broker.ListenAddr)
		return
	}

	// TLS
	var cert tls.Certificate
	cert, err = tls.LoadX509KeyPair(this.cf.Broker.TlsServerCert,
		this.cf.Broker.TlsServerKey)
	if err != nil {
		return
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"mqtt"},
	}
	listener, err = tls.Listen("tcp", this.cf.Broker.TlsListenAddr, cfg)
	log.Info("Accepting TLS client conn on %s", this.cf.Broker.TlsListenAddr)
	return
}
