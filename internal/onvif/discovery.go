package onvif

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

const discoveryAddress = "239.255.255.250:3702"

type DiscoveredDevice struct {
	EndpointReference string   `json:"endpoint_reference,omitempty"`
	XAddrs            []string `json:"xaddrs"`
	Scopes            []string `json:"scopes,omitempty"`
	Types             []string `json:"types,omitempty"`
	From              string   `json:"from,omitempty"`
}

func Discover(ctx context.Context, timeout time.Duration) ([]DiscoveredDevice, error) {
	if timeout <= 0 { timeout = 3 * time.Second }
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	target, err := net.ResolveUDPAddr("udp4", discoveryAddress)
	if err != nil { return nil, err }

	probe := []byte(discoveryProbe())
	type packet struct {
		data []byte
		from string
	}
	packets := make(chan packet, 64)
	var wg sync.WaitGroup
	sent := 0

	for _, localIP := range discoveryIPv4Addresses() {
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP:localIP, Port:0})
		if err != nil { continue }
		sent++
		wg.Add(1)
		go func(c *net.UDPConn) {
			defer wg.Done()
			defer c.Close()
			_, _ = c.WriteToUDP(probe, target)
			buf := make([]byte, 64*1024)
			for {
				deadline := time.Now().Add(400 * time.Millisecond)
				if end, ok := ctx.Deadline(); ok && end.Before(deadline) { deadline = end }
				_ = c.SetReadDeadline(deadline)
				n, from, err := c.ReadFromUDP(buf)
				if err != nil {
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						select {
						case <-ctx.Done():
							return
						default:
							_, _ = c.WriteToUDP(probe, target)
							continue
						}
					}
					return
				}
				copyBuf := append([]byte(nil), buf[:n]...)
				select {
				case packets <- packet{data:copyBuf, from:from.String()}:
				case <-ctx.Done():
					return
				}
			}
		}(conn)
	}

	if sent == 0 {
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP:net.IPv4zero, Port:0})
		if err != nil { return nil, err }
		sent++
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer conn.Close()
			_, _ = conn.WriteToUDP(probe, target)
			buf := make([]byte, 64*1024)
			for {
				_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				n, from, err := conn.ReadFromUDP(buf)
				if err != nil {
					select {
					case <-ctx.Done(): return
					default: continue
					}
				}
				select {
				case packets <- packet{data:append([]byte(nil), buf[:n]...), from:from.String()}:
				case <-ctx.Done(): return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(packets)
	}()

	found := make(map[string]DiscoveredDevice)
	for p := range packets {
		device, err := parseProbeMatch(p.data)
		if err != nil || len(device.XAddrs) == 0 { continue }
		device.From = p.from
		key := device.EndpointReference
		if key == "" { key = strings.Join(device.XAddrs, "|") }
		if existing, ok := found[key]; ok {
			existing.XAddrs = uniqueStrings(append(existing.XAddrs, device.XAddrs...))
			existing.Scopes = uniqueStrings(append(existing.Scopes, device.Scopes...))
			existing.Types = uniqueStrings(append(existing.Types, device.Types...))
			found[key] = existing
		} else {
			found[key] = device
		}
	}

	out := make([]DiscoveredDevice, 0, len(found))
	for _, item := range found {
		sort.Strings(item.XAddrs)
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].XAddrs) == 0 || len(out[j].XAddrs) == 0 { return out[i].EndpointReference < out[j].EndpointReference }
		return out[i].XAddrs[0] < out[j].XAddrs[0]
	})
	return out, nil
}

func discoveryIPv4Addresses() []net.IP {
	var out []net.IP
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagMulticast == 0 { continue }
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil { continue }
			if v4 := ip.To4(); v4 != nil { out = append(out, append(net.IP(nil), v4...)) }
		}
	}
	return out
}

func discoveryProbe() string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope"
 xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing"
 xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
 xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
 <e:Header>
  <w:MessageID>uuid:%s</w:MessageID>
  <w:To e:mustUnderstand="true">urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To>
  <w:Action e:mustUnderstand="true">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action>
 </e:Header>
 <e:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></e:Body>
</e:Envelope>`, randomHexID())
}

func parseProbeMatch(data []byte) (DiscoveredDevice, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	var device DiscoveredDevice
	for {
		token, err := decoder.Token()
		if err != nil {
			if len(device.XAddrs) > 0 { return device, nil }
			return DiscoveredDevice{}, err
		}
		start, ok := token.(xml.StartElement)
		if !ok { continue }
		switch start.Name.Local {
		case "Address":
			var value string
			if decoder.DecodeElement(&value, &start) == nil && strings.HasPrefix(strings.TrimSpace(value), "urn:uuid:") {
				device.EndpointReference = strings.TrimSpace(value)
			}
		case "XAddrs":
			var value string
			if decoder.DecodeElement(&value, &start) == nil { device.XAddrs = uniqueStrings(strings.Fields(value)) }
		case "Scopes":
			var value string
			if decoder.DecodeElement(&value, &start) == nil { device.Scopes = uniqueStrings(strings.Fields(value)) }
		case "Types":
			var value string
			if decoder.DecodeElement(&value, &start) == nil { device.Types = uniqueStrings(strings.Fields(value)) }
		}
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] { continue }
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func randomHexID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil { return fmt.Sprintf("%d", time.Now().UnixNano()) }
	return hex.EncodeToString(b[:])
}
