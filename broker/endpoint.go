package broker

import (
	log "github.com/funkygao/log4go"
	"github.com/funkygao/mhub/config"
	proto "github.com/funkygao/mqttmsg"
	"net"
	"time"
)

type endpoint struct {
	stats *stats

	cf      config.PeersConfig
	host    string   // host of other node, not myself
	conn    net.Conn // outbound conn to other node
	jobs    chan job
	alive   bool
	running bool
}

func newEndpoint(host string, cf config.PeersConfig, s *stats) (this *endpoint) {
	return &endpoint{
		stats:   s,
		cf:      cf,
		host:    host,
		jobs:    make(chan job, cf.QueueLen),
		alive:   true,
		running: true,
	}
}

func (this *endpoint) stop() {
	this.running = false
}

func (this *endpoint) start() {
	for this.running {
		this.runSession()

		// TODO sleep between retry? what about data lost during retry?
		log.Info("peer[%s] restarting", this.host)
	}
}

func (this *endpoint) runSession() {
	defer func() {
		if this.conn != nil {
			this.conn.Close()
		}
		this.alive = false
		close(this.jobs)
	}()

	var err error
	this.conn, err = net.Dial("tcp", this.host)
	if err != nil {
		log.Error(err)
		return
	}

	this.alive = true
	log.Info("peer[%s] connected", this.host)

	tcpConn, _ := this.conn.(*net.TCPConn)
	tcpConn.SetNoDelay(this.cf.TcpNoDelay)
	tcpConn.SetKeepAlive(this.cf.Keepalive)

	// consume jobs and send to subscription clients
	for job := range this.jobs {
		this.conn.SetWriteDeadline(time.Now().Add(this.cf.IoTimeout))
		err = job.m.Encode(this.conn) // replicated to peer
		if err != nil {
			log.Error(err)
			return
		}

		this.stats.replicated()
		this.stats.addRepl(job.m)
	}
}

func (this *endpoint) submit(m proto.Message) {
	if !this.alive {
		//log.Error("peer[%s] already died, %T %+v", this.host, m, m)
		return
	}

	if this.cf.BuffOverflowStrategy == config.BufferOverflowBlock {
		this.jobs <- job{m: m}
		return
	}

	// config.BufferOverflowDiscard
	select {
	case this.jobs <- job{m: m}:
	default:
		log.Debug("peer[%s]: outbound(%d) full, discard %T", this.host,
			len(this.jobs), m)
	}

}
