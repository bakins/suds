suds - simple dns server

Written to learn etcd, dns, etc.  Most of the etcd based dns servers did not fit my need or are too "ambitious."  I needed simple service discovery via SRV lookups but I also needed to support  "legacy" applications such as tftp clients, etc.

This is a work in progress.

Services are under "/services" and "nodes" are under "/nodes"

Let's add a couple of nodes:

`etcdctl set /nodes/foo '{"ip": "10.10.1.2"}'`
`etcdctl set /nodes/bar '{"ip": "10.10.1.3"}'`


Now we can look them up:

`dig @127.0.0.1 -p 15353 foo.nodes.suds.local.`

which will give us:
```
...
;; ANSWER SECTION:
foo.nodes.suds.local.	0	IN	A	10.10.1.1
...
```

Note that the ttl is 0. You can use the `-ttl` flag to change this. You should delegate your domain to suds.

Let's add a service:

`etcdctl set /services/www/2 '{"target": "foo.nodes."}'`

You can also set weight, priotiry, etc.  Since the target ends in ".nodes.", this tells suds to look up the node and include it in the additional section.

`dig @127.0.0.1 -p 15353 www.services.suds.local SRV`

```
...
;; ANSWER SECTION:
www.services.suds.local. 0	IN	SRV	0 0 0 foo.nodes.suds.local.

;; ADDITIONAL SECTION:
www.services.suds.local. 0	IN	A	10.10.1.1
...
```

We can add the other node:
`etcdctl set /services/www/3 '{"target": "bar.nodes."}'`

`dig @127.0.0.1 -p 15353 www.services.suds.local SRV`

```
...
;; ANSWER SECTION:
www.services.suds.local. 0	IN	SRV	0 0 0 bar.nodes.suds.local.
www.services.suds.local. 0	IN	SRV	0 0 0 foo.nodes.suds.local.

;; ADDITIONAL SECTION:
www.services.suds.local. 0	IN	A	10.10.1.3
www.services.suds.local. 0	IN	A	10.10.1.1
...
```

Any targets that do not end with ".nodes" or is not found is just not included in the additional section.

Note: XXX should the target have a flag as to whether it is a node and just use the base name for it.  We can not include it when not found, etc.  Or perhaps do the opposite - if the target does not include a "." then assume it is a node?

TODO: support RFC style (_service _protocol lookups)

The key we are setting in etcd is arbitrary (ie, 2, 3) but should be unique for the service - perhaps use the node name.  Also, you may want to include a ttl and have something ping it to ensure only "live" nodes are included.


For "legacy" applications that do not support using SRV, a roundrobin of nodes is returned:

`dig @127.0.0.1 -p 15353 www.services.suds.local`

```
...
;; ANSWER SECTION:
www.services.suds.local. 0	IN	A	10.10.1.3
www.services.suds.local. 0	IN	A	10.10.1.1
...
```

Aknowledgements:

DNS interfaced inspired by https://www.consul.io

Huge thanks to http://github.com/miekg/dns






