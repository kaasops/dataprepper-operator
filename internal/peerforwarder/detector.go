// Package peerforwarder detects stateful processors and generates peer forwarder configuration.
package peerforwarder

import "fmt"

const (
	// PeerForwarderPort is the Data Prepper peer forwarder port.
	PeerForwarderPort = 4994
	// DiscoveryModeDNS is the DNS-based peer discovery mode.
	DiscoveryModeDNS = "dns"
)

var statefulProcessors = map[string]bool{
	"aggregate":            true,
	"service_map":          true,
	"service_map_stateful": true,
	"otel_traces":          true,
	"otel_trace_raw":       true,
	"otel_traces_raw":      true,
}

// NeedsForwarding returns true if any pipeline contains a stateful processor.
func NeedsForwarding(processors [][]map[string]any) bool {
	for _, pipelineProcs := range processors {
		for _, proc := range pipelineProcs {
			for name := range proc {
				if statefulProcessors[name] {
					return true
				}
			}
		}
	}
	return false
}

// GenerateConfig returns peer_forwarder config for data-prepper-config.yaml.
func GenerateConfig(name, namespace string) map[string]any {
	return map[string]any{
		"peer_forwarder": map[string]any{
			"discovery_mode": DiscoveryModeDNS,
			"domain_name":    fmt.Sprintf("%s-headless.%s.svc.cluster.local", name, namespace),
			"port":           PeerForwarderPort,
		},
	}
}
