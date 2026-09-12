package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const remoteTLSDefaultPort = "443"

// remoteEndpoint keeps the address used for TCP separate from the name sent
// in the TLS ClientHello. In particular, a port must never be included in SNI
// and an IP address must not be sent as a DNS SNI name.
type remoteEndpoint struct {
	Host    string
	Port    string
	Address string
	SNI     string
}

// parseRemoteEndpoint accepts a hostname, hostname:port, IPv4, or IPv6
// address. A bare IPv6 address is deliberately treated as an address without
// a port; a port for IPv6 must use the usual [address]:port notation.
func parseRemoteEndpoint(value string) (remoteEndpoint, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return remoteEndpoint{}, fmt.Errorf("远程地址不能为空")
	}

	host := value
	port := remoteTLSDefaultPort

	switch {
	case strings.HasPrefix(value, "["):
		// SplitHostPort requires a port. Accept [::1] as the bracketed form
		// with the default port, but reject other malformed bracket syntax.
		if strings.HasSuffix(value, "]") {
			host = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
			if host == "" || net.ParseIP(host) == nil {
				return remoteEndpoint{}, fmt.Errorf("远程地址 %q 不是有效的括号 IPv6 地址", value)
			}
		} else {
			var err error
			host, port, err = net.SplitHostPort(value)
			if err != nil {
				return remoteEndpoint{}, fmt.Errorf("远程地址 %q 格式无效: %w", value, err)
			}
			if net.ParseIP(host) == nil {
				return remoteEndpoint{}, fmt.Errorf("远程地址 %q 的主机不是有效 IP 地址", value)
			}
		}
	case net.ParseIP(value) != nil:
		// This check must precede colon handling so bare IPv6 is not
		// mistaken for host:port.
		host = value
	case strings.Count(value, ":") == 1:
		var err error
		host, port, err = net.SplitHostPort(value)
		if err != nil {
			return remoteEndpoint{}, fmt.Errorf("远程地址 %q 格式无效: %w", value, err)
		}
		if host == "" {
			return remoteEndpoint{}, fmt.Errorf("远程地址 %q 缺少主机名", value)
		}
	case strings.Contains(value, ":"):
		return remoteEndpoint{}, fmt.Errorf("远程地址 %q 的 IPv6 端口必须使用 [地址]:端口格式", value)
	}

	if err := validateRemotePort(port); err != nil {
		return remoteEndpoint{}, fmt.Errorf("远程地址 %q: %w", value, err)
	}
	endpoint := remoteEndpoint{
		Host:    host,
		Port:    port,
		Address: net.JoinHostPort(host, port),
	}
	if net.ParseIP(host) == nil {
		endpoint.SNI = host
	}
	return endpoint, nil
}

func validateRemotePort(port string) error {
	if port == "" {
		return fmt.Errorf("端口不能为空")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("端口 %q 无效（应为 1-65535）", port)
	}
	return nil
}

func printCertInfo(cert *x509.Certificate) {
	fmt.Printf("----- 证书信息 -----\n")
	fmt.Printf("主题(Subject): %s\n", cert.Subject)
	fmt.Printf("颁发者(Issuer): %s\n", cert.Issuer)
	fmt.Printf("序列号(Serial): %s\n", cert.SerialNumber)
	fmt.Printf("签名算法(Signature): %s\n", cert.SignatureAlgorithm)
	fmt.Printf("有效期(Valid From): %s\n", cert.NotBefore.Format("2006-01-02 15:04:05"))
	fmt.Printf("到期时间(Valid To): %s\n", cert.NotAfter.Format("2006-01-02 15:04:05"))
	fmt.Printf("剩余有效期: %v\n", time.Until(cert.NotAfter))
	fmt.Printf("DNS名称(SAN): %s\n", strings.Join(cert.DNSNames, ", "))
	fmt.Printf("IP地址: %v\n", cert.IPAddresses)
	fmt.Printf("Email: %v\n", cert.EmailAddresses)
	fmt.Printf("颁发用途: %v\n", cert.ExtKeyUsage)
}

// certificateChainTrusted verifies the peer chain against the system roots.
// The TLS handshake intentionally skips verification so diagnostics can still
// inspect self-signed, untrusted, and expired certificates.
func certificateChainTrusted(certificates []*x509.Certificate) (bool, error) {
	if len(certificates) == 0 {
		return false, fmt.Errorf("未获取到远程证书")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return false, fmt.Errorf("读取系统根证书: %w", err)
	}
	if roots == nil {
		return false, fmt.Errorf("系统根证书池为空")
	}
	intermediates := x509.NewCertPool()
	for _, cert := range certificates[1:] {
		intermediates.AddCert(cert)
	}
	_, err = certificates[0].Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		// Leave DNSName empty: hostname matching is reported separately.
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

func certificateHostnameMatches(cert *x509.Certificate, host string) (bool, error) {
	if cert == nil {
		return false, fmt.Errorf("未获取到远程证书")
	}
	if err := cert.VerifyHostname(host); err != nil {
		return false, err
	}
	return true, nil
}

func certificateCurrentlyValid(cert *x509.Certificate, now time.Time) bool {
	if cert == nil {
		return false
	}
	return !now.Before(cert.NotBefore) && !now.After(cert.NotAfter)
}

func reportCertificateStatus(certificates []*x509.Certificate, host string, now time.Time) {
	if len(certificates) == 0 {
		fmt.Printf("证书链信任: 否 (未获取到远程证书)\n")
		fmt.Printf("主机名匹配: 否 (未获取到远程证书)\n")
		fmt.Printf("有效期状态: 无法判断（未获取到远程证书）\n")
		return
	}
	trusted, trustErr := certificateChainTrusted(certificates)
	hostnameMatched, hostnameErr := certificateHostnameMatches(certificates[0], host)
	currentlyValid := certificateCurrentlyValid(certificates[0], now)

	if trusted {
		fmt.Printf("证书链信任: 是\n")
	} else {
		fmt.Printf("证书链信任: 否 (%v)\n", trustErr)
	}
	if hostnameMatched {
		fmt.Printf("主机名匹配: 是\n")
	} else {
		fmt.Printf("主机名匹配: 否 (%v)\n", hostnameErr)
	}
	if currentlyValid {
		fmt.Printf("有效期状态: 有效\n")
	} else {
		fmt.Printf("有效期状态: 无效（尚未生效或已过期）\n")
	}
}

// checkRemoteDomain connects to an endpoint while retaining the old CLI
// entry point. The endpoint's hostname is used as SNI only when it is a DNS
// name; IP addresses use an empty SNI value.
func checkRemoteDomain(domain string) error {
	endpoint, err := parseRemoteEndpoint(domain)
	if err != nil {
		return err
	}
	return checkRemoteEndpoint(endpoint, endpoint.SNI)
}

// checkRemoteEndpoint is split out so callers/tests can use an explicit SNI
// for virtual hosts served on a custom connection address.
func checkRemoteEndpoint(endpoint remoteEndpoint, serverName string) error {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", endpoint.Address, &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true, // diagnostics must inspect invalid certificates
	})
	if err != nil {
		return fmt.Errorf("无法连接远程主机 %s: %w", endpoint.Address, err)
	}
	defer conn.Close()
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return fmt.Errorf("未获取到远程证书")
	}

	cert := state.PeerCertificates[0]
	fmt.Printf("远程主机: %s\n", endpoint.Address)
	if serverName != "" {
		fmt.Printf("TLS SNI: %s\n", serverName)
	} else {
		fmt.Printf("TLS SNI: （未发送，连接目标为 IP 地址）\n")
	}
	fmt.Printf("证书链长度: %d\n", len(state.PeerCertificates))
	printCertInfo(cert)
	verificationName := endpoint.Host
	if serverName != "" {
		verificationName = serverName
	}
	reportCertificateStatus(state.PeerCertificates, verificationName, time.Now())
	fmt.Printf("TLS 握手完成；以上验证状态分别独立报告。\n")
	return nil
}
