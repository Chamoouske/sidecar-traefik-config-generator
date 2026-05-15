package hostrule

import "fmt"

// Config holds parameters for building a Host rule.
type Config struct {
	ServiceName  string
	NodeHostname string
	DomainSuffix string // default: "lab"
}

// HostRule represents a generated Traefik Host rule.
type HostRule struct {
	Host      string // "service.node.lab"
	Rule      string // "Host(`service.node.lab`)"
	HasCustom bool   // true se foi sobrescrito por label
}

// Build gera o HostRule automaticamente no formato "service.node.{domainSuffix}".
func Build(serviceName, nodeHostname, domainSuffix string) *HostRule {
	if domainSuffix == "" {
		domainSuffix = "lab"
	}
	host := fmt.Sprintf("%s.%s.%s", serviceName, nodeHostname, domainSuffix)
	return &HostRule{
		Host: host,
		Rule: fmt.Sprintf("Host(`%s`)", host),
	}
}

// BuildFromLabels verifica se existe label traefik.federation.host.
// Se existir, usa esse valor customizado em vez do auto-generated.
func BuildFromLabels(serviceName, nodeHostname, domainSuffix string, labels map[string]string) *HostRule {
	if labels != nil {
		if host, ok := labels["traefik.federation.host"]; ok && host != "" {
			return &HostRule{
				Host:      host,
				Rule:      fmt.Sprintf("Host(`%s`)", host),
				HasCustom: true,
			}
		}
	}
	return Build(serviceName, nodeHostname, domainSuffix)
}
