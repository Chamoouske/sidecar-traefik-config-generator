package agent

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

type PeerInfo struct {
	Name string
	IP   string
}

func RegisterService(instance, serviceType string, port int, txtRecords []string) (*mdns.Server, error) {
	hostIP := detectHostIP()
	if hostIP == "" {
		return nil, fmt.Errorf("could not detect host IP")
	}

	service, err := mdns.NewMDNSService(
		instance,
		serviceType,
		"local.",
		"",
		port,
		[]net.IP{net.ParseIP(hostIP)},
		txtRecords,
	)
	if err != nil {
		return nil, fmt.Errorf("new mdns service: %w", err)
	}

	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		return nil, fmt.Errorf("mdns server: %w", err)
	}

	return server, nil
}

func discoverPeers(serviceType string, timeout time.Duration) ([]PeerInfo, error) {
	entriesCh := make(chan *mdns.ServiceEntry, 16)

	params := &mdns.QueryParam{
		Service:     serviceType,
		Domain:      "local",
		Timeout:     timeout,
		Entries:     entriesCh,
		DisableIPv6: true,
	}

	var peers []PeerInfo

	errChan := make(chan error, 1)
	go func() {
		errChan <- mdns.Query(params)
	}()

	for entry := range entriesCh {
		peerName := ""
		for _, field := range entry.InfoFields {
			if strings.HasPrefix(field, "node_name=") {
				peerName = strings.TrimPrefix(field, "node_name=")
				break
			}
		}

		if peerName == "" {
			continue
		}

		ip := ""
		if entry.AddrV4 != nil {
			ip = entry.AddrV4.String()
		}
		if ip == "" {
			continue
		}

		peers = append(peers, PeerInfo{Name: peerName, IP: ip})
	}

	if err := <-errChan; err != nil {
		return peers, err
	}

	return peers, nil
}

func WatchPeers(ctx context.Context, nodeName, serviceType string, interval time.Duration) <-chan PeerInfo {
	peerCh := make(chan PeerInfo, 16)

	go func() {
		defer close(peerCh)

		known := make(map[string]bool)

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			entriesCh := make(chan *mdns.ServiceEntry, 16)

			params := &mdns.QueryParam{
				Service:     serviceType,
				Domain:      "local",
				Timeout:     interval,
				Entries:     entriesCh,
				DisableIPv6: true,
			}

			seen := make(map[string]bool)

			done := make(chan error, 1)
			go func() {
				done <- mdns.Query(params)
			}()

			for entry := range entriesCh {
				peerName := ""
				for _, field := range entry.InfoFields {
					if strings.HasPrefix(field, "node_name=") {
						peerName = strings.TrimPrefix(field, "node_name=")
						break
					}
				}

				if peerName == "" || peerName == nodeName {
					seen[entry.Name] = true
					continue
				}

				seen[entry.Name] = true

				if !known[entry.Name] {
					known[entry.Name] = true

					ip := ""
					if entry.AddrV4 != nil {
						ip = entry.AddrV4.String()
					}
					if ip == "" {
						continue
					}

					select {
					case peerCh <- PeerInfo{Name: peerName, IP: ip}:
					case <-ctx.Done():
						return
					}
				}
			}

			for name := range known {
				if !seen[name] {
					delete(known, name)
				}
			}

			<-done

			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()

	return peerCh
}

func detectHostIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.To4() == nil {
				continue
			}
			return ipnet.IP.String()
		}
	}

	return ""
}
