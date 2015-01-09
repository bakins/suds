package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strings"
	"time"

	etcdErr "github.com/coreos/etcd/error"
	"github.com/coreos/go-etcd/etcd"
	"github.com/miekg/dns"
)

type (
	Server struct {
		etcd   *etcd.Client
		domain string
		prefix string
		ttl    uint32
	}

	Record struct {
		Priority uint16 `json:priority`
		Weight   uint16 `json:weight`
		Port     uint16 `json:port`
		Target   string `json:target`
	}

	Node struct {
		IP net.IP `json:ip`
	}
)

func IsKeyNotFound(err error) bool {
	e, ok := err.(*etcd.EtcdError)
	return ok && e.ErrorCode == etcdErr.EcodeKeyNotFound
}

func NameError(w dns.ResponseWriter, req *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(req)
	m.SetRcode(req, dns.RcodeNameError)
	w.WriteMsg(m)
}

func (s *Server) GetNode(name string) (*Node, error) {
	name = strings.TrimSuffix(name, ".nodes.")

	key := filepath.Join("/", s.prefix, "nodes", name)
	log.Printf("GetNode: %s\n", key)

	resp, err := s.etcd.Get(key, false, false)

	if err != nil {
		return nil, err
	}

	var node Node
	err = json.Unmarshal([]byte(resp.Node.Value), &node)
	if err != nil {
		return nil, err
	}

	if node.IP == nil {
		return nil, nil
	}
	return &node, nil
}

func (s *Server) GetService(name string) ([]*Record, error) {
	name = strings.TrimSuffix(name, ".services.")

	key := filepath.Join("/", s.prefix, "services", name)
	log.Printf("GetService: %s\n", key)
	resp, err := s.etcd.Get(key, false, true)

	if err != nil {
		return nil, err
	}

	records := make([]*Record, 0, len(resp.Node.Nodes))

	for _, n := range resp.Node.Nodes {
		var record Record
		err := json.Unmarshal([]byte(n.Value), &record)
		if err != nil {
			log.Printf("%s: %s", n.Key, err)
			continue
		}

		// should match against a regex?
		if record.Target == "" {
			continue
		}

		records = append(records, &record)
	}

	return records, nil
}

func (s *Server) ServicesA(w dns.ResponseWriter, r *dns.Msg, name string) (*dns.Msg, error) {
	fmt.Println("ServicesA start")

	records, err := s.GetService(name)
	if err != nil {
		log.Printf("%s: %s", name, err)
		return nil, err
	}

	if len(records) == 0 {
		return nil, nil
	}
	m := &dns.Msg{}
	m.SetReply(r)

	header := dns.RR_Header{
		Name:   r.Question[0].Name,
		Rrtype: r.Question[0].Qtype,
		Class:  r.Question[0].Qclass,
		Ttl:    s.ttl,
	}

	m.Answer = make([]dns.RR, 0, len(records))
	for _, record := range records {
		if !strings.HasSuffix(record.Target, ".nodes.") {
			// we can't look this up
			log.Printf("%s: not .nodes.", record.Target)
			continue
		}

		node, err := s.GetNode(record.Target)
		if err != nil {
			log.Printf("%s: %s", record.Target, err)
			continue
		}

		answer := &dns.A{
			Hdr: header,
			A:   node.IP,
		}

		m.Answer = append(m.Answer, answer)
	}

	return m, nil

}

func (s *Server) ServicesSRV(w dns.ResponseWriter, r *dns.Msg, name string) (*dns.Msg, error) {
	fmt.Println("ServicesSRV start")

	records, err := s.GetService(name)
	if err != nil {
		log.Printf("%s: %s", name, err)
		return nil, err
	}

	if len(records) == 0 {
		return nil, nil
	}

	m := &dns.Msg{}
	m.SetReply(r)

	header := dns.RR_Header{
		Name:   r.Question[0].Name,
		Rrtype: r.Question[0].Qtype,
		Class:  r.Question[0].Qclass,
		Ttl:    s.ttl,
	}

	m.Answer = make([]dns.RR, 0, len(records))
	m.Extra = make([]dns.RR, 0, len(records))
	for _, record := range records {
		answer := &dns.SRV{
			Hdr:      header,
			Priority: record.Priority,
			Weight:   record.Weight,
			Port:     record.Port,
			Target:   record.Target + s.domain,
		}

		m.Answer = append(m.Answer, answer)

		if !strings.HasSuffix(record.Target, ".nodes.") {
			fmt.Println("Not nodes")
			continue
		}

		node, err := s.GetNode(record.Target)
		if err != nil {
			log.Printf("%s: %s", record.Target, err)
			continue
		}

		extra := &dns.A{
			Hdr: dns.RR_Header{
				Name:   r.Question[0].Name,
				Rrtype: dns.TypeA,
				Class:  r.Question[0].Qclass,
				Ttl:    s.ttl,
			},
			A: node.IP,
		}

		m.Extra = append(m.Extra, extra)
	}

	return m, nil

}

func (s *Server) NodesA(w dns.ResponseWriter, r *dns.Msg, name string) (*dns.Msg, error) {
	fmt.Println("NodesA start")

	node, err := s.GetNode(name)
	if err != nil {
		log.Printf("%s: %s", name, err)
		return nil, err
	}

	m := &dns.Msg{}
	m.SetReply(r)

	header := dns.RR_Header{
		Name:   r.Question[0].Name,
		Rrtype: r.Question[0].Qtype,
		Class:  r.Question[0].Qclass,
		Ttl:    s.ttl,
	}

	m.Answer = []dns.RR{
		&dns.A{
			Hdr: header,
			A:   node.IP,
		},
	}

	return m, nil

}
func (s *Server) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {

	question := r.Question[0].Name
	name := strings.TrimSuffix(question, s.domain)
	parts := strings.Split(name, ".")
	parts = parts[:len(parts)-1]

	if len(parts) != 2 {
		log.Printf("invalid query: %s", question)
		NameError(w, r)
		return
	}

	rType := parts[1]
	qType := r.Question[0].Qtype
	fmt.Println(question, name, rType)

	var m *dns.Msg
	var err error
	switch rType {
	case "services":
		switch qType {
		case dns.TypeA:
			m, err = s.ServicesA(w, r, name)
		case dns.TypeSRV:
			m, err = s.ServicesSRV(w, r, name)
		}
	case "nodes":
		switch qType {
		case dns.TypeA:
			m, err = s.NodesA(w, r, name)
		}
	}

	if err != nil {
		fmt.Println(err)
		if IsKeyNotFound(err) {
			NameError(w, r)
		} else {
			dns.HandleFailed(w, r)
		}
		return
	}

	if m == nil {
		log.Printf("%s: not found", question)
		NameError(w, r)
		return
	}

	w.WriteMsg(m)
}

func main() {

	ttl := flag.Uint("ttl", 0, "DNS TTL for responses")
	domain := flag.String("domain", "suds.local.", "domain - must end with '.'")
	eaddr := flag.String("etcd", "http://localhost:4001", "etcd address")
	prefix := flag.String("prefix", "/", "etcd prefix")
	address := flag.String("address", ":15353", "UDP address to listen")
	flag.Parse()

	e := etcd.NewClient(([]string{*eaddr}))

	s := &Server{
		etcd:   e,
		domain: *domain,
		prefix: *prefix,
		ttl:    uint32(*ttl),
	}

	dns.Handle(s.domain, s)

	server := &dns.Server{
		Addr:         *address,
		Net:          "udp",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Fatal(server.ListenAndServe())

}
