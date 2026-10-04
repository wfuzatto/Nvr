package onvif

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	nsDevice = "http://www.onvif.org/ver10/device/wsdl"
	nsMedia  = "http://www.onvif.org/ver10/media/wsdl"
	nsMedia2 = "http://www.onvif.org/ver20/media/wsdl"
	nsPTZ    = "http://www.onvif.org/ver20/ptz/wsdl"
	nsSchema = "http://www.onvif.org/ver10/schema"
)

type Client struct {
	Endpoint string
	Username string
	Password string
	HTTP     *http.Client
	services map[string]string
}

type DeviceInformation struct {
	Manufacturer    string `json:"manufacturer,omitempty"`
	Model           string `json:"model,omitempty"`
	FirmwareVersion string `json:"firmware_version,omitempty"`
	SerialNumber    string `json:"serial_number,omitempty"`
	HardwareID      string `json:"hardware_id,omitempty"`
}

type Service struct {
	Namespace string `json:"namespace"`
	XAddr     string `json:"xaddr"`
}

type Profile struct {
	Token          string `json:"token"`
	Name           string `json:"name,omitempty"`
	Encoding       string `json:"encoding,omitempty"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
	FrameRateLimit int    `json:"frame_rate_limit,omitempty"`
	BitrateLimit   int    `json:"bitrate_limit,omitempty"`
	PTZ            bool   `json:"ptz"`
	MediaVersion   int    `json:"media_version"`
}

type PTZStatus struct {
	Pan        float64 `json:"pan,omitempty"`
	Tilt       float64 `json:"tilt,omitempty"`
	Zoom       float64 `json:"zoom,omitempty"`
	PanTiltMove string  `json:"pan_tilt_move,omitempty"`
	ZoomMove    string  `json:"zoom_move,omitempty"`
	UtcTime     string  `json:"utc_time,omitempty"`
}

func NewClient(endpoint, username, password string, timeout time.Duration) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Hostname() == "" { return nil, errors.New("invalid ONVIF endpoint") }
	if u.Scheme != "http" && u.Scheme != "https" { return nil, errors.New("ONVIF endpoint must use http or https") }
	if u.User != nil {
		if username == "" { username = u.User.Username() }
		if password == "" { password, _ = u.User.Password() }
		u.User = nil
	}
	if timeout <= 0 { timeout = 7 * time.Second }
	return &Client{
		Endpoint:u.String(), Username:username, Password:password,
		HTTP:&http.Client{Timeout:timeout},
		services:make(map[string]string),
	}, nil
}

func (c *Client) DeviceInformation(ctx context.Context) (DeviceInformation, error) {
	body, err := c.soap(ctx, c.Endpoint, nsDevice+"/GetDeviceInformation", `<tds:GetDeviceInformation/>`)
	if err != nil { return DeviceInformation{}, err }
	return parseDeviceInformation(body)
}

func (c *Client) Services(ctx context.Context) ([]Service, error) {
	body, err := c.soap(ctx, c.Endpoint, nsDevice+"/GetServices", `<tds:GetServices><tds:IncludeCapability>false</tds:IncludeCapability></tds:GetServices>`)
	if err != nil { return nil, err }
	services, err := parseServices(body)
	if err != nil { return nil, err }
	for _, service := range services {
		if service.Namespace != "" && service.XAddr != "" { c.services[service.Namespace] = service.XAddr }
	}
	return services, nil
}

func (c *Client) Capabilities(ctx context.Context) ([]Service, error) {
	body, err := c.soap(ctx, c.Endpoint, nsDevice+"/GetCapabilities", `<tds:GetCapabilities><tds:Category>All</tds:Category></tds:GetCapabilities>`)
	if err != nil { return nil, err }
	services, err := parseCapabilities(body)
	if err != nil { return nil, err }
	for _, service := range services {
		if service.Namespace != "" && service.XAddr != "" { c.services[service.Namespace] = service.XAddr }
	}
	return services, nil
}

func (c *Client) loadServices(ctx context.Context) {
	if len(c.services) > 0 && (c.services[nsMedia] != "" || c.services[nsMedia2] != "") { return }
	_, _ = c.Services(ctx)
	if c.services[nsMedia] == "" && c.services[nsMedia2] == "" {
		_, _ = c.Capabilities(ctx)
	}
}

func (c *Client) Profiles(ctx context.Context) ([]Profile, error) {
	c.loadServices(ctx)

	if xaddr := c.services[nsMedia2]; xaddr != "" {
		body, err := c.soap(ctx, xaddr, nsMedia2+"/GetProfiles", `<tr2:GetProfiles/>`)
		if err == nil {
			if profiles, parseErr := parseProfiles(body, 2); parseErr == nil && len(profiles) > 0 { return profiles, nil }
		}
	}
	xaddr := c.services[nsMedia]
	if xaddr == "" { xaddr = c.Endpoint }
	body, err := c.soap(ctx, xaddr, nsMedia+"/GetProfiles", `<trt:GetProfiles/>`)
	if err != nil { return nil, err }
	return parseProfiles(body, 1)
}

func (c *Client) StreamURI(ctx context.Context, profileToken string, mediaVersion int) (string, error) {
	if strings.TrimSpace(profileToken) == "" { return "", errors.New("profile token is required") }
	c.loadServices(ctx)
	if mediaVersion == 2 && c.services[nsMedia2] != "" {
		body := `<tr2:GetStreamUri><tr2:Protocol>RTSP</tr2:Protocol><tr2:ProfileToken>`+xmlEscape(profileToken)+`</tr2:ProfileToken></tr2:GetStreamUri>`
		resp, err := c.soap(ctx, c.services[nsMedia2], nsMedia2+"/GetStreamUri", body)
		if err != nil { return "", err }
		return findElementText(resp, "Uri")
	}
	xaddr := c.services[nsMedia]
	if xaddr == "" { xaddr = c.Endpoint }
	body := `<trt:GetStreamUri><trt:StreamSetup><tt:Stream>RTP-Unicast</tt:Stream><tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport></trt:StreamSetup><trt:ProfileToken>`+xmlEscape(profileToken)+`</trt:ProfileToken></trt:GetStreamUri>`
	resp, err := c.soap(ctx, xaddr, nsMedia+"/GetStreamUri", body)
	if err != nil { return "", err }
	return findElementText(resp, "Uri")
}

func (c *Client) SnapshotURI(ctx context.Context, profileToken string, mediaVersion int) (string, error) {
	if strings.TrimSpace(profileToken) == "" { return "", errors.New("profile token is required") }
	c.loadServices(ctx)
	if mediaVersion == 2 && c.services[nsMedia2] != "" {
		body := `<tr2:GetSnapshotUri><tr2:ProfileToken>`+xmlEscape(profileToken)+`</tr2:ProfileToken></tr2:GetSnapshotUri>`
		resp, err := c.soap(ctx, c.services[nsMedia2], nsMedia2+"/GetSnapshotUri", body)
		if err == nil {
			if uri, findErr := findElementText(resp, "Uri"); findErr == nil { return uri, nil }
		}
	}
	xaddr := c.services[nsMedia]
	if xaddr == "" { return "", errors.New("ONVIF Media service unavailable") }
	body := `<trt:GetSnapshotUri><trt:ProfileToken>`+xmlEscape(profileToken)+`</trt:ProfileToken></trt:GetSnapshotUri>`
	resp, err := c.soap(ctx, xaddr, nsMedia+"/GetSnapshotUri", body)
	if err != nil { return "", err }
	return findElementText(resp, "Uri")
}

func (c *Client) PTZStatus(ctx context.Context, profileToken string) (PTZStatus, error) {
	c.loadServices(ctx)
	xaddr := c.services[nsPTZ]
	if xaddr == "" { return PTZStatus{}, errors.New("ONVIF PTZ service unavailable") }
	body := `<tptz:GetStatus><tptz:ProfileToken>`+xmlEscape(profileToken)+`</tptz:ProfileToken></tptz:GetStatus>`
	resp, err := c.soap(ctx, xaddr, nsPTZ+"/GetStatus", body)
	if err != nil { return PTZStatus{}, err }
	return parsePTZStatus(resp)
}

func (c *Client) PTZContinuousMove(ctx context.Context, profileToken string, pan, tilt, zoom float64, timeout time.Duration) error {
	c.loadServices(ctx)
	xaddr := c.services[nsPTZ]
	if xaddr == "" { return errors.New("ONVIF PTZ service unavailable") }
	pan = clamp(pan); tilt = clamp(tilt); zoom = clamp(zoom)
	var velocity strings.Builder
	if pan != 0 || tilt != 0 {
		fmt.Fprintf(&velocity, `<tt:PanTilt x="%s" y="%s"/>`, floatXML(pan), floatXML(tilt))
	}
	if zoom != 0 { fmt.Fprintf(&velocity, `<tt:Zoom x="%s"/>`, floatXML(zoom)) }
	if velocity.Len() == 0 { return errors.New("PTZ velocity cannot be zero") }
	timeoutXML := ""
	if timeout > 0 { timeoutXML = "<tptz:Timeout>PT"+strconv.FormatFloat(timeout.Seconds(),'f',3,64)+"S</tptz:Timeout>" }
	body := `<tptz:ContinuousMove><tptz:ProfileToken>`+xmlEscape(profileToken)+`</tptz:ProfileToken><tptz:Velocity>`+velocity.String()+`</tptz:Velocity>`+timeoutXML+`</tptz:ContinuousMove>`
	_, err := c.soap(ctx, xaddr, nsPTZ+"/ContinuousMove", body)
	return err
}

func (c *Client) PTZStop(ctx context.Context, profileToken string, panTilt, zoom bool) error {
	c.loadServices(ctx)
	xaddr := c.services[nsPTZ]
	if xaddr == "" { return errors.New("ONVIF PTZ service unavailable") }
	body := `<tptz:Stop><tptz:ProfileToken>`+xmlEscape(profileToken)+`</tptz:ProfileToken><tptz:PanTilt>`+strconv.FormatBool(panTilt)+`</tptz:PanTilt><tptz:Zoom>`+strconv.FormatBool(zoom)+`</tptz:Zoom></tptz:Stop>`
	_, err := c.soap(ctx, xaddr, nsPTZ+"/Stop", body)
	return err
}

func (c *Client) soap(ctx context.Context, endpoint, action, body string) ([]byte, error) {
	envelope, err := c.envelope(body)
	if err != nil { return nil, err }
	resp, err := c.doSOAP(ctx, endpoint, action, envelope, "")
	if err != nil { return nil, err }

	if resp.StatusCode == http.StatusUnauthorized && c.Username != "" {
		challenges := resp.Header.Values("WWW-Authenticate")
		_ = resp.Body.Close()
		auth, err := buildHTTPAuthorization(challenges, c.Username, c.Password, http.MethodPost, endpoint)
		if err != nil { return nil, err }
		resp, err = c.doSOAP(ctx, endpoint, action, envelope, auth)
		if err != nil { return nil, err }
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil { return nil, err }
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fault := parseSOAPFault(payload)
		if fault == "" { fault = strings.TrimSpace(string(payload)) }
		if len(fault) > 500 { fault = fault[:500] }
		return nil, fmt.Errorf("ONVIF SOAP %d: %s", resp.StatusCode, fault)
	}
	if fault := parseSOAPFault(payload); fault != "" { return nil, errors.New("ONVIF SOAP fault: "+fault) }
	return payload, nil
}

func (c *Client) doSOAP(ctx context.Context, endpoint, action string, envelope []byte, authorization string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(envelope))
	if err != nil { return nil, err }
	req.Header.Set("Content-Type", `application/soap+xml; charset=utf-8; action="`+action+`"`)
	req.Header.Set("SOAPAction", `"`+action+`"`)
	req.Header.Set("User-Agent", "NVR/0.3")
	if authorization != "" { req.Header.Set("Authorization", authorization) }
	return c.HTTP.Do(req)
}

func (c *Client) envelope(body string) ([]byte, error) {
	header := ""
	if c.Username != "" {
		nonce := make([]byte, 20)
		if _, err := rand.Read(nonce); err != nil { return nil, err }
		created := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		h := sha1.New()
		_, _ = h.Write(nonce)
		_, _ = h.Write([]byte(created))
		_, _ = h.Write([]byte(c.Password))
		digest := base64.StdEncoding.EncodeToString(h.Sum(nil))
		header = `<wsse:Security s:mustUnderstand="1"><wsse:UsernameToken><wsse:Username>`+xmlEscape(c.Username)+`</wsse:Username><wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">`+digest+`</wsse:Password><wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">`+base64.StdEncoding.EncodeToString(nonce)+`</wsse:Nonce><wsu:Created>`+created+`</wsu:Created></wsse:UsernameToken></wsse:Security>`
	}
	envelope := `<?xml version="1.0" encoding="UTF-8"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tr2="http://www.onvif.org/ver20/media/wsdl" xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema" xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"><s:Header>`+header+`</s:Header><s:Body>`+body+`</s:Body></s:Envelope>`
	return []byte(envelope), nil
}

func parseDeviceInformation(payload []byte) (DeviceInformation, error) {
	var out DeviceInformation
	values := collectElementTexts(payload, map[string]bool{"Manufacturer":true,"Model":true,"FirmwareVersion":true,"SerialNumber":true,"HardwareId":true,"HardwareID":true})
	out.Manufacturer = first(values["Manufacturer"])
	out.Model = first(values["Model"])
	out.FirmwareVersion = first(values["FirmwareVersion"])
	out.SerialNumber = first(values["SerialNumber"])
	out.HardwareID = first(values["HardwareId"])
	if out.HardwareID == "" { out.HardwareID = first(values["HardwareID"]) }
	return out, nil
}

func parseServices(payload []byte) ([]Service, error) {
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	var out []Service
	var current *Service
	for {
		token, err := decoder.Token()
		if err == io.EOF { break }
		if err != nil { return nil, err }
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local == "Service" { current = &Service{}; continue }
			if current == nil { continue }
			if t.Name.Local == "Namespace" || t.Name.Local == "XAddr" {
				var value string
				if err := decoder.DecodeElement(&value, &t); err != nil { return nil, err }
				if t.Name.Local == "Namespace" { current.Namespace = strings.TrimSpace(value) } else { current.XAddr = strings.TrimSpace(value) }
			}
		case xml.EndElement:
			if t.Name.Local == "Service" && current != nil {
				if current.Namespace != "" && current.XAddr != "" { out = append(out, *current) }
				current = nil
			}
		}
	}
	return out, nil
}

func parseCapabilities(payload []byte) ([]Service, error) {
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	var stack []string
	var out []Service
	seen := make(map[string]bool)
	for {
		token, err := decoder.Token()
		if err == io.EOF { break }
		if err != nil { return nil, err }
		switch t := token.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			if t.Name.Local != "XAddr" { continue }
			var value string
			if err := decoder.DecodeElement(&value, &t); err != nil { return nil, err }
			stack = stack[:len(stack)-1]
			value = strings.TrimSpace(value)
			if value == "" || len(stack) == 0 { continue }
			parent := stack[len(stack)-1]
			namespace := ""
			switch parent {
			case "Media": namespace = nsMedia
			case "Media2": namespace = nsMedia2
			case "PTZ": namespace = nsPTZ
			}
			if namespace != "" && !seen[namespace+"|"+value] {
				seen[namespace+"|"+value] = true
				out = append(out, Service{Namespace:namespace, XAddr:value})
			}
		case xml.EndElement:
			if len(stack)>0 { stack=stack[:len(stack)-1] }
		}
	}
	return out,nil
}

func parseProfiles(payload []byte, mediaVersion int) ([]Profile, error) {
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	var out []Profile
	var current *Profile
	stack := []string{}
	for {
		token, err := decoder.Token()
		if err == io.EOF { break }
		if err != nil { return nil, err }
		switch t := token.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			if t.Name.Local == "Profiles" || (mediaVersion == 2 && t.Name.Local == "Profiles") {
				token := attr(t.Attr, "token")
				if token != "" { current = &Profile{Token:token, MediaVersion:mediaVersion} }
			}
			if current == nil { continue }
			switch t.Name.Local {
			case "Name","Encoding","Width","Height","FrameRateLimit","BitrateLimit":
				var value string
				if err := decoder.DecodeElement(&value, &t); err != nil { return nil, err }
				stack = stack[:len(stack)-1]
				value = strings.TrimSpace(value)
				switch t.Name.Local {
				case "Name": if current.Name == "" { current.Name = value }
				case "Encoding": if current.Encoding == "" { current.Encoding = value }
				case "Width": if current.Width == 0 { current.Width, _ = strconv.Atoi(value) }
				case "Height": if current.Height == 0 { current.Height, _ = strconv.Atoi(value) }
				case "FrameRateLimit": if current.FrameRateLimit == 0 { current.FrameRateLimit, _ = strconv.Atoi(value) }
				case "BitrateLimit": if current.BitrateLimit == 0 { current.BitrateLimit, _ = strconv.Atoi(value) }
				}
			case "PTZConfiguration":
				current.PTZ = true
			}
		case xml.EndElement:
			if current != nil && t.Name.Local == "Profiles" {
				if current.Token != "" { out = append(out, *current) }
				current = nil
			}
			if len(stack) > 0 { stack = stack[:len(stack)-1] }
		}
	}
	if len(out) == 0 { return nil, errors.New("ONVIF returned no media profiles") }
	return out, nil
}

func parsePTZStatus(payload []byte) (PTZStatus, error) {
	var status PTZStatus
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	for {
		token, err := decoder.Token()
		if err == io.EOF { break }
		if err != nil { return status, err }
		start, ok := token.(xml.StartElement)
		if !ok { continue }
		switch start.Name.Local {
		case "PanTilt":
			status.Pan, _ = strconv.ParseFloat(attr(start.Attr,"x"),64)
			status.Tilt, _ = strconv.ParseFloat(attr(start.Attr,"y"),64)
		case "Zoom":
			status.Zoom, _ = strconv.ParseFloat(attr(start.Attr,"x"),64)
		case "PanTiltMoveStatus":
			var v string; _ = decoder.DecodeElement(&v,&start); status.PanTiltMove=strings.TrimSpace(v)
		case "ZoomMoveStatus":
			var v string; _ = decoder.DecodeElement(&v,&start); status.ZoomMove=strings.TrimSpace(v)
		case "UtcTime":
			var v string; _ = decoder.DecodeElement(&v,&start); status.UtcTime=strings.TrimSpace(v)
		}
	}
	return status, nil
}

func findElementText(payload []byte, local string) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	for {
		token, err := decoder.Token()
		if err == io.EOF { break }
		if err != nil { return "", err }
		start, ok := token.(xml.StartElement)
		if ok && start.Name.Local == local {
			var value string
			if err := decoder.DecodeElement(&value,&start); err != nil { return "", err }
			value = strings.TrimSpace(value)
			if value != "" { return value, nil }
		}
	}
	return "", fmt.Errorf("ONVIF response missing %s", local)
}

func collectElementTexts(payload []byte, wanted map[string]bool) map[string][]string {
	out := make(map[string][]string)
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	for {
		token, err := decoder.Token()
		if err != nil { break }
		start, ok := token.(xml.StartElement)
		if !ok || !wanted[start.Name.Local] { continue }
		var value string
		if decoder.DecodeElement(&value,&start) == nil {
			out[start.Name.Local] = append(out[start.Name.Local], strings.TrimSpace(value))
		}
	}
	return out
}

func parseSOAPFault(payload []byte) string {
	values := collectElementTexts(payload, map[string]bool{"Text":true,"Reason":true,"faultstring":true,"Value":true})
	for _, key := range []string{"Text","faultstring","Reason","Value"} {
		if value := first(values[key]); value != "" { return value }
	}
	return ""
}

func buildHTTPAuthorization(challenges []string, username, password, method, endpoint string) (string, error) {
	for _, challenge := range challenges {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(challenge)), "digest") { continue }
		u, _ := url.Parse(endpoint)
		uri := "/"
		if u != nil && u.RequestURI() != "" { uri = u.RequestURI() }
		return digestAuthorization(challenge, username, password, method, uri)
	}
	for _, challenge := range challenges {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(challenge)), "basic") {
			return "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)), nil
		}
	}
	return "", errors.New("unsupported ONVIF HTTP authentication challenge")
}

func digestAuthorization(challenge, username, password, method, uri string) (string, error) {
	raw := strings.TrimSpace(challenge)
	if len(raw) < 6 { return "", errors.New("invalid Digest challenge") }
	params := parseAuthParams(strings.TrimSpace(raw[6:]))
	realm, nonce := params["realm"], params["nonce"]
	if realm == "" || nonce == "" { return "", errors.New("invalid Digest challenge") }
	algorithm := strings.ToUpper(params["algorithm"])
	if algorithm == "" { algorithm = "MD5" }
	if algorithm != "MD5" && algorithm != "MD5-SESS" { return "", fmt.Errorf("unsupported Digest algorithm %q", algorithm) }

	cnonceBytes := make([]byte,8)
	if _, err := rand.Read(cnonceBytes); err != nil { return "", err }
	cnonce := hex.EncodeToString(cnonceBytes)
	nc := "00000001"
	qop := ""
	for _, value := range strings.Split(params["qop"],",") {
		if strings.EqualFold(strings.TrimSpace(value),"auth") { qop="auth"; break }
	}
	ha1 := md5Hex(username+":"+realm+":"+password)
	if algorithm == "MD5-SESS" { ha1 = md5Hex(ha1+":"+nonce+":"+cnonce) }
	ha2 := md5Hex(method+":"+uri)
	response := md5Hex(ha1+":"+nonce+":"+ha2)
	if qop != "" { response = md5Hex(ha1+":"+nonce+":"+nc+":"+cnonce+":"+qop+":"+ha2) }

	var b strings.Builder
	fmt.Fprintf(&b,`Digest username=%q, realm=%q, nonce=%q, uri=%q, response=%q`,username,realm,nonce,uri,response)
	if opaque:=params["opaque"]; opaque!="" { fmt.Fprintf(&b,`, opaque=%q`,opaque) }
	if qop!="" { fmt.Fprintf(&b,`, qop=%s, nc=%s, cnonce=%q`,qop,nc,cnonce) }
	fmt.Fprintf(&b,`, algorithm=%s`,algorithm)
	return b.String(),nil
}

func parseAuthParams(raw string) map[string]string {
	out:=make(map[string]string)
	var b strings.Builder
	quoted:=false
	flush:=func(){
		part:=strings.TrimSpace(b.String()); b.Reset()
		if part=="" { return }
		kv:=strings.SplitN(part,"=",2)
		if len(kv)!=2 { return }
		out[strings.ToLower(strings.TrimSpace(kv[0]))]=strings.Trim(strings.TrimSpace(kv[1]),`"`)
	}
	for _,r:=range raw {
		switch r {
		case '"': quoted=!quoted; b.WriteRune(r)
		case ',': if quoted { b.WriteRune(r) } else { flush() }
		default: b.WriteRune(r)
		}
	}
	flush()
	return out
}

func md5Hex(value string) string { sum:=md5.Sum([]byte(value)); return hex.EncodeToString(sum[:]) }

func xmlEscape(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b,[]byte(value))
	return b.String()
}

func attr(attrs []xml.Attr, local string) string {
	for _, a := range attrs { if a.Name.Local==local { return a.Value } }
	return ""
}
func first(values []string) string { if len(values)==0 { return "" }; return values[0] }
func clamp(v float64) float64 { if v>1 { return 1 }; if v< -1 { return -1 }; return v }
func floatXML(v float64) string { return strconv.FormatFloat(v,'f',4,64) }
