package certificates

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"net"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

func userCertTemplate(username string) *x509.CertificateRequest {
	return &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: username,
		},
	}
}

func serverCertTemplate(name string, addresses []string) *x509.CertificateRequest {
	var dnsNames []string
	var ips []net.IP
	for _, a := range addresses {
		if ip := net.ParseIP(a); ip != nil {
			ips = append(ips, ip)
		} else {
			dnsNames = append(dnsNames, a)
		}
	}
	return &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: name,
		},
		DNSNames:    dnsNames,
		IPAddresses: ips,
	}
}

// This helps us detect when a certificate needs to be regenerated. It should
// include all of our template fields in the functions above.
func certMatchesTemplate(cert *x509.Certificate, template *x509.CertificateRequest) bool {
	certDNSNames := ds.NewSet(cert.DNSNames...)
	templateDNSNames := ds.NewSet(template.DNSNames...)

	certIPs := ds.NewSet[string]()
	templateIPs := ds.NewSet[string]()

	for _, ip := range cert.IPAddresses {
		certIPs.Add(ip.String())
	}
	for _, ip := range template.IPAddresses {
		templateIPs.Add(ip.String())
	}

	return cert.Subject.CommonName == template.Subject.CommonName &&
		certDNSNames.Equal(templateDNSNames) &&
		certIPs.Equal(templateIPs)
}

func certPEMMatchesTemplate(certPEM []byte, template *x509.CertificateRequest) (bool, error) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return false, errors.New("failed to decode certificate PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return false, fmt.Errorf("failed to parse stored cert: %w", err)
	}
	return certMatchesTemplate(cert, template), nil
}
