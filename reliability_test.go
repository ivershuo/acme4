package main

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/miekg/dns"
)

func testCertificatePair(t *testing.T, commonName string) ([]byte, []byte) {
	return testCertificatePairNames(t, []string{commonName})
}

func testCertificatePairNames(t *testing.T, names []string) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM
}

func TestSaveCertificatePairValidatesMatchBeforeReplacing(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "example.com.crt")
	keyPath := filepath.Join(dir, "example.com.key")
	oldCert, oldKey := testCertificatePair(t, "old.example.com")
	if err := saveCertificatePair(certPath, keyPath, oldCert, oldKey); err != nil {
		t.Fatal(err)
	}
	newCert, _ := testCertificatePair(t, "new.example.com")
	_, wrongKey := testCertificatePair(t, "wrong.example.com")
	if err := saveCertificatePair(certPath, keyPath, newCert, wrongKey); err == nil {
		t.Fatal("saveCertificatePair() expected mismatch error")
	}
	gotCert, _ := os.ReadFile(certPath)
	gotKey, _ := os.ReadFile(keyPath)
	if string(gotCert) != string(oldCert) || string(gotKey) != string(oldKey) {
		t.Fatal("existing certificate pair changed after validation failure")
	}
}

func TestInspectCertificatePairReasons(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	cert, key := testCertificatePair(t, "example.com")
	if err := saveCertificatePair(certPath, keyPath, cert, key); err != nil {
		t.Fatal(err)
	}

	status, err := inspectCertificatePair(certPath, keyPath, []string{"EXAMPLE.COM."}, 0)
	if err != nil || status.NeedsRenew {
		t.Fatalf("valid reordered/normalized names status=%+v err=%v", status, err)
	}
	status, err = inspectCertificatePair(certPath, keyPath, []string{"example.com", "*.example.com"}, 0)
	if err != nil || !status.NeedsRenew || status.Reason != "san_changed" {
		t.Fatalf("SAN change status=%+v err=%v", status, err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	status, err = inspectCertificatePair(certPath, keyPath, []string{"example.com"}, 0)
	if err != nil || status.Reason != "private_key_missing" {
		t.Fatalf("missing key status=%+v err=%v", status, err)
	}
}

func TestInspectCertificatePairDetectsMismatchedKey(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	cert, _ := testCertificatePair(t, "example.com")
	_, otherKey := testCertificatePair(t, "other.example.com")
	if err := os.WriteFile(certPath, cert, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, otherKey, 0600); err != nil {
		t.Fatal(err)
	}
	status, err := inspectCertificatePair(certPath, keyPath, []string{"example.com"}, 0)
	if err != nil || status.Reason != "certificate_key_mismatch" {
		t.Fatalf("mismatched key status=%+v err=%v", status, err)
	}
}

func TestInspectCertificatePairIgnoresSANOrderButDetectsRemoval(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	cert, key := testCertificatePairNames(t, []string{"example.com", "*.example.com"})
	if err := saveCertificatePair(certPath, keyPath, cert, key); err != nil {
		t.Fatal(err)
	}
	status, err := inspectCertificatePair(certPath, keyPath, []string{"*.example.com", "example.com"}, 0)
	if err != nil || status.NeedsRenew {
		t.Fatalf("SAN order should not renew: status=%+v err=%v", status, err)
	}
	status, err = inspectCertificatePair(certPath, keyPath, []string{"example.com"}, 0)
	if err != nil || status.Reason != "san_changed" {
		t.Fatalf("SAN removal status=%+v err=%v", status, err)
	}
}

func TestInspectCertificatePairTreatsDamageAsRenewable(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	if err := os.WriteFile(certPath, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	status, err := inspectCertificatePair(certPath, filepath.Join(dir, "key.pem"), []string{"example.com"}, 30)
	if err != nil || !status.NeedsRenew || status.Reason != "certificate_invalid" {
		t.Fatalf("damaged certificate status=%+v err=%v", status, err)
	}
}

func TestInstallAndDeployDoesNotRunHookWhenPersistenceFails(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "hook-ran")
	cert, key := testCertificatePair(t, "example.com")
	badCertPath := filepath.Join(dir, "certificate-is-a-directory")
	if err := os.Mkdir(badCertPath, 0700); err != nil {
		t.Fatal(err)
	}
	_, err := installAndDeploy(badCertPath, filepath.Join(dir, "example.com.key"), cert, key, []string{"touch " + marker}, "example.com")
	if err == nil || !strings.Contains(err.Error(), "certificate persistence") {
		t.Fatalf("installAndDeploy() error = %v", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("deployment hook ran after persistence failure: %v", statErr)
	}
}

func TestOperationLockRejectsConcurrentProcessFlow(t *testing.T) {
	dir := t.TempDir()
	first, err := acquireOperationLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := acquireOperationLock(dir); err == nil || !strings.Contains(err.Error(), "另一个签发任务") {
		t.Fatalf("second acquireOperationLock() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	first = nil
	third, err := acquireOperationLock(dir)
	if err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
	_ = third.Close()
}

func TestOperationLockAcrossProcesses(t *testing.T) {
	if os.Getenv("ACME4_LOCK_HELPER") == "1" {
		lock, err := acquireOperationLock(os.Getenv("ACME4_LOCK_DIR"))
		if err != nil {
			os.Exit(3)
		}
		_, _ = fmt.Fprintln(os.Stdout, "locked")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		_ = lock.Close()
		os.Exit(0)
	}

	dir := t.TempDir()
	first := exec.Command(os.Args[0], "-test.run=TestOperationLockAcrossProcesses")
	first.Env = append(os.Environ(), "ACME4_LOCK_HELPER=1", "ACME4_LOCK_DIR="+dir)
	stdin, err := first.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := first.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	if !bufio.NewScanner(stdout).Scan() {
		t.Fatal("first helper did not acquire the lock")
	}
	defer func() {
		_, _ = stdin.Write([]byte("release\n"))
		_ = stdin.Close()
		_ = first.Wait()
	}()

	second := exec.Command(os.Args[0], "-test.run=TestOperationLockAcrossProcesses")
	second.Env = append(os.Environ(), "ACME4_LOCK_HELPER=1", "ACME4_LOCK_DIR="+dir)
	err = second.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("second process should fail on held lock, got %v", err)
	}
}

func TestDNSResolverNormalization(t *testing.T) {
	got, err := normalizeResolvers([]string{"192.0.2.1", "2001:db8::53", "[2001:db8::54]:5353", "resolver.example:5300"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.0.2.1:53", "[2001:db8::53]:53", "[2001:db8::54]:5353", "resolver.example:5300"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ParseNameservers() = %v, want %v", got, want)
	}
}

func TestDNSResolverNormalizationRejectsInvalidPort(t *testing.T) {
	if _, err := normalizeResolvers([]string{"127.0.0.1:not-a-port"}); err == nil {
		t.Fatal("normalizeResolvers() expected invalid port error")
	}
}

func TestInstallAndDeployReportsHookFailure(t *testing.T) {
	dir := t.TempDir()
	cert, key := testCertificatePair(t, "example.com")
	_, err := installAndDeploy(filepath.Join(dir, "example.com.crt"), filepath.Join(dir, "example.com.key"), cert, key, []string{"exit 7"}, "example.com")
	if err == nil || !strings.Contains(err.Error(), "deployment") {
		t.Fatalf("installAndDeploy() error = %v", err)
	}
}

func TestCustomResolverReceivesCNAMEQuery(t *testing.T) {
	packetConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skip("sandbox does not allow binding a local UDP test server")
		}
		t.Fatal(err)
	}
	var queries atomic.Int32
	server := &dns.Server{PacketConn: packetConn, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, request *dns.Msg) {
		queries.Add(1)
		response := new(dns.Msg)
		response.SetReply(request)
		if len(request.Question) > 0 && request.Question[0].Name == "_acme-challenge.example.com." {
			response.Answer = append(response.Answer, &dns.CNAME{
				Hdr:    dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60},
				Target: "example-com.acme.validation.test.",
			})
		}
		_ = w.WriteMsg(response)
	})}
	go func() { _ = server.ActivateAndServe() }()
	t.Cleanup(func() { _ = server.Shutdown() })

	dns01.ClearFqdnCache()
	if err := dns01.AddRecursiveNameservers([]string{packetConn.LocalAddr().String()})(nil); err != nil {
		t.Fatal(err)
	}
	info := dns01.GetChallengeInfo("example.com", "key-auth")
	if info.EffectiveFQDN != "example-com.acme.validation.test." {
		t.Fatalf("EffectiveFQDN = %q", info.EffectiveFQDN)
	}
	if queries.Load() == 0 {
		t.Fatal("custom resolver did not receive a query")
	}
}

func TestRunAggregatesDomainFailures(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	config := fmt.Sprintf("email: test@example.com\ncert_dir: %q\naccount_dir: %q\ndomains:\n  - names: [one.example]\n    provider: missing-one\n  - names: [two.example]\n    provider: missing-two\n", filepath.Join(dir, "certs"), filepath.Join(dir, "accounts"))
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	err := run(configPath)
	if err == nil || !strings.Contains(err.Error(), "one.example") || !strings.Contains(err.Error(), "two.example") {
		t.Fatalf("run() did not aggregate both domain failures: %v", err)
	}
}
