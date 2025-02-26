package certificates

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"net"
)

func userCertTemplate(username string) *x509.CertificateRequest {
	return &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: username,
		},
	}
}

// func PostgresUser(username string) *x509.CertificateRequest {
// 	return &x509.CertificateRequest{
// 		Subject: pkix.Name{
// 			CommonName: username,
// 		},

// 	}
// }

// func EtcdUser(username string) *x509.CertificateRequest {
// 	return &x509.CertificateRequest{
// 		Subject: pkix.Name{
// 			CommonName: username,
// 		},
// 	}
// }

func serverCertTemplate(hostname string, dnsNames, ipAddresses []string) *x509.CertificateRequest {
	ips := make([]net.IP, len(ipAddresses))
	for idx, ip := range ipAddresses {
		ips[idx] = net.ParseIP(ip)
	}
	return &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: hostname,
		},
		DNSNames:    dnsNames,
		IPAddresses: ips,
	}
}
