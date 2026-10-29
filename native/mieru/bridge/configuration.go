package bridge

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlcommon"
	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

type ConfigurationError struct{ Message string }

func (e *ConfigurationError) Error() string { return e.Message }
func configError(message string) error      { return &ConfigurationError{message} }

func portBinding(value string, protocol string) (*pb.PortBinding, error) {
	p, ok := pb.TransportProtocol_value[strings.ToUpper(protocol)]
	if !ok || (p != int32(pb.TransportProtocol_TCP) && p != int32(pb.TransportProtocol_UDP)) {
		return nil, configError("Transport must be TCP or UDP")
	}
	parts := strings.Split(value, "-")
	if len(parts) < 1 || len(parts) > 2 {
		return nil, configError("Enter a port or a range such as 4012-4021")
	}
	ports := make([]int, len(parts))
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > 65535 {
			return nil, configError("Ports must be integers between 1 and 65535")
		}
		ports[i] = n
	}
	binding := &pb.PortBinding{Protocol: pb.TransportProtocol(p).Enum()}
	if len(ports) == 1 {
		binding.Port = proto.Int32(int32(ports[0]))
	} else {
		if ports[0] > ports[1] {
			return nil, configError("Port range start must not exceed its end")
		}
		binding.PortRange = proto.String(fmt.Sprintf("%d-%d", ports[0], ports[1]))
	}
	return binding, nil
}

func validServerHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	if strings.Count(host, ".") == 3 && strings.Trim(host, "0123456789.") == "" {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}

func validateProfile(p *pb.ClientProfile) error {
	if p.GetProfileName() == "" {
		return configError("Profile name is required")
	}
	if p.GetUser().GetName() == "" {
		return configError("Username is required")
	}
	if p.GetUser().GetPassword() == "" && p.GetUser().GetHashedPassword() == "" {
		return configError("Password is required")
	}
	if len(p.GetUser().GetPassword()) > 64 {
		return configError("Password must not exceed 64 bytes")
	}
	if len(p.GetUser().GetName()) > 64 {
		return configError("Username must not exceed 64 bytes")
	}
	if hash := p.GetUser().GetHashedPassword(); hash != "" {
		decoded, err := hex.DecodeString(hash)
		if err != nil || len(decoded) != 32 {
			return configError("Hashed password must contain 64 hexadecimal characters")
		}
	}
	if len(p.GetServers()) == 0 {
		return configError("At least one server is required")
	}
	if p.GetDialer() != nil {
		return configError("Client API does not support an external profile dialer")
	}
	for _, server := range p.GetServers() {
		if server.GetIpAddress() != "" && net.ParseIP(server.GetIpAddress()) == nil {
			return configError("Server IP address is invalid")
		}
		host := server.GetDomainName()
		if host == "" {
			host = server.GetIpAddress()
		}
		if !validServerHost(host) {
			return configError("Server must be an IPv4, IPv6 address or a valid domain without a URL or port")
		}
		if server.GetIpAddress() != "" && server.GetDomainName() != "" {
			return configError("Specify either server IP address or domain, not both")
		}
		if len(server.GetPortBindings()) == 0 {
			return configError("Server port is required")
		}
		for _, b := range server.GetPortBindings() {
			if b.GetPort() != 0 && b.GetPortRange() != "" {
				return configError("Specify either port or port range, not both")
			}
			v := b.GetPortRange()
			if v == "" {
				v = strconv.Itoa(int(b.GetPort()))
			}
			if _, err := portBinding(v, b.GetProtocol().String()); err != nil {
				return err
			}
		}
	}
	if p.GetMtu() != 0 && (p.GetMtu() < 1280 || p.GetMtu() > 1500) {
		return configError("MTU must be Automatic (0) or between 1280 and 1500")
	}
	if _, ok := pb.MultiplexingLevel_name[int32(p.GetMultiplexing().GetLevel())]; !ok {
		return configError("Unknown multiplexing level")
	}
	if _, ok := pb.HandshakeMode_name[int32(p.GetHandshakeMode())]; !ok {
		return configError("Unknown handshake mode")
	}
	if err := appctlcommon.ValidateClientConfigSingleProfile(p); err != nil {
		return configError("Profile contains unsupported user, traffic-pattern or transport settings")
	}
	return nil
}

type clashProfile struct {
	Name           string      `json:"name"`
	Type           string      `json:"type"`
	Server         string      `json:"server"`
	Port           json.Number `json:"port"`
	PortRange      string      `json:"port-range"`
	Transport      string      `json:"transport"`
	Username       string      `json:"username"`
	Password       string      `json:"password"`
	MTU            int32       `json:"mtu"`
	Multiplexing   string      `json:"multiplexing"`
	Handshake      string      `json:"handshake-mode"`
	TrafficPattern string      `json:"traffic-pattern"`
}

func fromClash(data []byte) (*pb.ClientProfile, error) {
	var c clashProfile
	if json.Unmarshal(data, &c) != nil {
		return nil, configError("Malformed Clash/Mihomo Mieru profile fields")
	}
	if c.Type != "mieru" {
		return nil, configError("Expected a Clash/Mihomo profile with type mieru")
	}
	if c.Port != "" && c.PortRange != "" {
		return nil, configError("Specify either port or port-range, not both")
	}
	port := c.PortRange
	if port == "" {
		port = string(c.Port)
	}
	transport := c.Transport
	if transport == "" {
		transport = "TCP"
	}
	binding, err := portBinding(port, transport)
	if err != nil {
		return nil, err
	}
	server := &pb.ServerEndpoint{PortBindings: []*pb.PortBinding{binding}}
	if net.ParseIP(c.Server) != nil {
		server.IpAddress = proto.String(c.Server)
	} else {
		server.DomainName = proto.String(c.Server)
	}
	p := &pb.ClientProfile{ProfileName: proto.String(c.Name), User: &pb.User{Name: proto.String(c.Username), Password: proto.String(c.Password)}, Servers: []*pb.ServerEndpoint{server}, Mtu: proto.Int32(c.MTU)}
	if c.Multiplexing != "" {
		v, ok := pb.MultiplexingLevel_value[c.Multiplexing]
		if !ok {
			return nil, configError("Unknown Clash/Mihomo multiplexing level")
		}
		p.Multiplexing = &pb.MultiplexingConfig{Level: pb.MultiplexingLevel(v).Enum()}
	}
	if c.Handshake != "" {
		v, ok := pb.HandshakeMode_value[c.Handshake]
		if !ok {
			return nil, configError("Unknown Clash/Mihomo handshake mode")
		}
		p.HandshakeMode = pb.HandshakeMode(v).Enum()
	}
	if c.TrafficPattern != "" {
		raw, err := base64.StdEncoding.DecodeString(c.TrafficPattern)
		if err != nil {
			return nil, configError("Invalid encoded traffic-pattern")
		}
		p.TrafficPattern = &pb.TrafficPattern{}
		if proto.Unmarshal(raw, p.TrafficPattern) != nil {
			return nil, configError("Invalid traffic-pattern structure")
		}
	}
	return p, nil
}

func fromURL(input string) (*pb.ClientConfig, error) {
	if strings.HasPrefix(input, "mieru://") {
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(input, "mieru://"))
		c := &pb.ClientConfig{}
		if err != nil || proto.Unmarshal(data, c) != nil {
			return nil, configError("Invalid mieru link: expected a base64 encoded official ClientConfig")
		}
		return c, nil
	}
	u, err := url.Parse(input)
	if err != nil || u.Scheme != "mierus" || u.Opaque != "" || u.Path != "" || u.Fragment != "" || u.Port() != "" {
		return nil, configError("Invalid mierus link structure")
	}
	if u.User == nil {
		return nil, configError("Mierus link must contain username and password")
	}
	password, _ := u.User.Password()
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, configError("Invalid mierus link query")
	}
	for key, values := range q {
		if key != "profile" && key != "port" && key != "protocol" && key != "mtu" && key != "multiplexing" && key != "handshake-mode" && key != "traffic-pattern" {
			return nil, configError("Unsupported mierus link parameter")
		}
		if key != "port" && key != "protocol" && len(values) != 1 {
			return nil, configError("Duplicate mierus link parameter")
		}
	}
	c := clashProfile{Name: q.Get("profile"), Type: "mieru", Server: u.Hostname(), Username: u.User.Username(), Password: password, Multiplexing: q.Get("multiplexing"), Handshake: q.Get("handshake-mode"), TrafficPattern: q.Get("traffic-pattern")}
	if q.Get("mtu") != "" {
		v, err := strconv.ParseInt(q.Get("mtu"), 10, 32)
		if err != nil {
			return nil, configError("Invalid link MTU")
		}
		c.MTU = int32(v)
	}
	ports, protocols := q["port"], q["protocol"]
	if len(ports) == 0 || len(ports) != len(protocols) {
		return nil, configError("Link port and protocol counts must match")
	}
	c.Port = "1"
	c.Transport = "TCP"
	data, _ := json.Marshal(c)
	p, err := fromClash(data)
	if err != nil {
		return nil, err
	}
	p.Servers[0].PortBindings = nil
	for i, port := range ports {
		b, err := portBinding(port, protocols[i])
		if err != nil {
			return nil, err
		}
		p.Servers[0].PortBindings = append(p.Servers[0].PortBindings, b)
	}
	return &pb.ClientConfig{Profiles: []*pb.ClientProfile{p}, ActiveProfile: p.ProfileName}, nil
}

func ParseConfiguration(data []byte) (*pb.ClientConfig, error) {
	if len(data) == 0 || len(data) > MaxConfigSize {
		return nil, configError("Configuration must be between 1 byte and 1 MiB")
	}
	data = bytes.TrimSpace(data)
	if !utf8.Valid(data) {
		return nil, configError("Configuration text must use valid UTF-8")
	}
	var config *pb.ClientConfig
	if bytes.HasPrefix(data, []byte("mieru://")) || bytes.HasPrefix(data, []byte("mierus://")) {
		var err error
		config, err = fromURL(string(data))
		if err != nil {
			return nil, err
		}
	} else {
		if !json.Valid(data) {
			var node yaml.Node
			decoder := yaml.NewDecoder(bytes.NewReader(data))
			if decoder.Decode(&node) != nil {
				return nil, configError("Input is not valid JSON or YAML")
			}
			var extra yaml.Node
			if decoder.Decode(&extra) != io.EOF {
				return nil, configError("Import one YAML document at a time")
			}
			count := 0
			var check func(*yaml.Node, int) bool
			check = func(n *yaml.Node, depth int) bool {
				count++
				if count > 20000 || depth > 32 || n.Kind == yaml.AliasNode {
					return false
				}
				for _, child := range n.Content {
					if !check(child, depth+1) {
						return false
					}
				}
				return true
			}
			if !check(&node, 0) {
				return nil, configError("YAML aliases or excessive nesting are not supported")
			}
			var value map[string]interface{}
			if node.Decode(&value) != nil || value == nil {
				return nil, configError("Expected a configuration object")
			}
			var err error
			data, err = json.Marshal(value)
			if err != nil {
				return nil, configError("YAML keys must be strings")
			}
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(data, &fields) != nil || fields == nil {
			return nil, configError("Expected a configuration object")
		}
		config = &pb.ClientConfig{}
		if _, ok := fields["profiles"]; ok {
			if protojson.Unmarshal(data, config) != nil {
				return nil, configError("Invalid official ClientConfig structure or enum value")
			}
		} else if _, ok := fields["proxies"]; ok {
			var proxies []json.RawMessage
			if json.Unmarshal(fields["proxies"], &proxies) != nil {
				return nil, configError("Clash proxies must be a list")
			}
			for _, raw := range proxies {
				var kind struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(raw, &kind) != nil {
					return nil, configError("Malformed Clash proxy entry")
				}
				if kind.Type != "mieru" {
					continue
				}
				p, err := fromClash(raw)
				if err != nil {
					return nil, err
				}
				config.Profiles = append(config.Profiles, p)
			}
			if len(config.Profiles) > 0 {
				config.ActiveProfile = config.Profiles[0].ProfileName
			}
		} else {
			p := &pb.ClientProfile{}
			if _, ok := fields["type"]; ok {
				var err error
				p, err = fromClash(data)
				if err != nil {
					return nil, err
				}
			} else if protojson.Unmarshal(data, p) != nil {
				return nil, configError("Invalid official ClientProfile structure or enum value")
			}
			config.Profiles = []*pb.ClientProfile{p}
			config.ActiveProfile = p.ProfileName
		}
	}
	if len(config.Profiles) == 0 {
		return nil, configError("No Mieru profiles found")
	}
	if len(config.Profiles) > 100 {
		return nil, configError("A maximum of 100 profiles is supported")
	}
	names := map[string]bool{}
	found := false
	for _, p := range config.Profiles {
		if err := validateProfile(p); err != nil {
			return nil, err
		}
		if names[p.GetProfileName()] {
			return nil, configError("Profile names must be unique")
		}
		names[p.GetProfileName()] = true
		if p.GetProfileName() == config.GetActiveProfile() {
			found = true
		}
	}
	if config.GetActiveProfile() == "" && len(config.Profiles) == 1 {
		config.ActiveProfile = config.Profiles[0].ProfileName
		found = true
	}
	if !found {
		return nil, configError("activeProfile must select one imported profile")
	}
	return config, nil
}

func NormalizeConfiguration(data []byte) ([]byte, error) {
	c, err := ParseConfiguration(data)
	if err != nil {
		return nil, err
	}
	out, err := protojson.Marshal(c)
	if err != nil {
		return nil, errors.New("Could not normalize configuration")
	}
	return out, nil
}
