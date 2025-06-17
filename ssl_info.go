package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strings"
	"time"
)

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

func checkRemoteDomain(domain string) error {
	host := domain
	if !strings.Contains(host, ":") {
		host = host + ":443"
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", host, &tls.Config{
		ServerName:         domain,
		InsecureSkipVerify: true,
	})
	if err != nil {
		return fmt.Errorf("无法连接远程主机: %v", err)
	}
	defer conn.Close()
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return fmt.Errorf("未获取到远程证书")
	}
	cert := state.PeerCertificates[0]
	fmt.Printf("远程主机: %s\n", domain)
	fmt.Printf("证书链长度: %d\n", len(state.PeerCertificates))
	printCertInfo(cert)
	return nil
}
