package onvif

import "testing"

func TestParseProbeMatch(t *testing.T) {
	raw := []byte(`<?xml version="1.0"?>
<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope">
<e:Body><ProbeMatches><ProbeMatch>
<EndpointReference><Address>urn:uuid:abc</Address></EndpointReference>
<Types>dn:NetworkVideoTransmitter</Types>
<Scopes>onvif://www.onvif.org/name/Camera onvif://www.onvif.org/location/city</Scopes>
<XAddrs>http://10.0.0.10/onvif/device_service http://10.0.0.10:80/onvif/device_service</XAddrs>
</ProbeMatch></ProbeMatches></e:Body></e:Envelope>`)
	got, err := parseProbeMatch(raw)
	if err != nil { t.Fatal(err) }
	if got.EndpointReference != "urn:uuid:abc" { t.Fatalf("endpoint=%q", got.EndpointReference) }
	if len(got.XAddrs) != 2 { t.Fatalf("xaddrs=%v", got.XAddrs) }
	if len(got.Scopes) != 2 { t.Fatalf("scopes=%v", got.Scopes) }
}
