package onvif

import (
	"strings"
	"testing"
)

func TestParseServices(t *testing.T) {
	payload := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><GetServicesResponse>
	<Service><Namespace>http://www.onvif.org/ver10/media/wsdl</Namespace><XAddr>http://10.0.0.1/onvif/Media</XAddr></Service>
	<Service><Namespace>http://www.onvif.org/ver20/ptz/wsdl</Namespace><XAddr>http://10.0.0.1/onvif/PTZ</XAddr></Service>
	</GetServicesResponse></s:Body></s:Envelope>`)
	services, err := parseServices(payload)
	if err != nil { t.Fatal(err) }
	if len(services) != 2 { t.Fatalf("services=%v", services) }
	if services[0].Namespace != nsMedia { t.Fatalf("namespace=%q", services[0].Namespace) }
}

func TestParseProfiles(t *testing.T) {
	payload := []byte(`<Envelope><Body><GetProfilesResponse>
	<Profiles token="main"><Name>Main Stream</Name><VideoEncoderConfiguration token="v1"><Encoding>H264</Encoding><Resolution><Width>1920</Width><Height>1080</Height></Resolution><RateControl><FrameRateLimit>25</FrameRateLimit><BitrateLimit>4096</BitrateLimit></RateControl></VideoEncoderConfiguration><PTZConfiguration token="p1"/></Profiles>
	<Profiles token="sub"><Name>Sub Stream</Name><VideoEncoderConfiguration token="v2"><Encoding>H264</Encoding><Resolution><Width>640</Width><Height>360</Height></Resolution></VideoEncoderConfiguration></Profiles>
	</GetProfilesResponse></Body></Envelope>`)
	profiles, err := parseProfiles(payload,1)
	if err != nil { t.Fatal(err) }
	if len(profiles)!=2 { t.Fatalf("profiles=%v",profiles) }
	if profiles[0].Token!="main" || profiles[0].Width!=1920 || profiles[0].Height!=1080 || !profiles[0].PTZ { t.Fatalf("main=%+v",profiles[0]) }
	if profiles[1].Token!="sub" || profiles[1].Width!=640 { t.Fatalf("sub=%+v",profiles[1]) }
}

func TestDigestAuthorization(t *testing.T) {
	auth, err := digestAuthorization(`Digest realm="cam", nonce="abc", qop="auth", algorithm=MD5`,"admin","secret","POST","/onvif/device_service")
	if err != nil { t.Fatal(err) }
	for _, part := range []string{"Digest ",`username="admin"`,`realm="cam"`,"response=","qop=auth"} {
		if !strings.Contains(auth,part) { t.Fatalf("missing %q in %q",part,auth) }
	}
}

func TestWSSEEnvelopeDoesNotExposePlainPassword(t *testing.T) {
	c, err := NewClient("http://10.0.0.1/onvif/device_service","admin","TopSecret123",0)
	if err != nil { t.Fatal(err) }
	env, err := c.envelope(`<tds:GetDeviceInformation/>`)
	if err != nil { t.Fatal(err) }
	text := string(env)
	if strings.Contains(text,"TopSecret123") { t.Fatal("plain password leaked in WS-Security envelope") }
	if !strings.Contains(text,"PasswordDigest") || !strings.Contains(text,"UsernameToken") { t.Fatalf("invalid envelope: %s",text) }
}

func TestParseCapabilities(t *testing.T) {
	payload := []byte(`<Envelope><Body><GetCapabilitiesResponse><Capabilities>
	<Media><XAddr>http://10.0.0.1/onvif/media</XAddr></Media>
	<PTZ><XAddr>http://10.0.0.1/onvif/ptz</XAddr></PTZ>
	</Capabilities></GetCapabilitiesResponse></Body></Envelope>`)
	services, err := parseCapabilities(payload)
	if err != nil { t.Fatal(err) }
	if len(services) != 2 { t.Fatalf("services=%v", services) }
	foundMedia, foundPTZ := false, false
	for _, service := range services {
		if service.Namespace == nsMedia && service.XAddr == "http://10.0.0.1/onvif/media" { foundMedia = true }
		if service.Namespace == nsPTZ && service.XAddr == "http://10.0.0.1/onvif/ptz" { foundPTZ = true }
	}
	if !foundMedia || !foundPTZ { t.Fatalf("missing capabilities: %+v", services) }
}
