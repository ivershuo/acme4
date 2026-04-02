package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	tchttp "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/http"
)

type mockUploadClient struct {
	send func(request tchttp.Request, response tchttp.Response) error
}

func (m mockUploadClient) Send(request tchttp.Request, response tchttp.Response) error {
	return m.send(request, response)
}

func TestUploaderUploadBuildsExpectedRequest(t *testing.T) {
	certPEM, keyPEM := mustCreateTestKeyPair(t)

	uploader := &TencentUploader{
		Client: mockUploadClient{
			send: func(request tchttp.Request, response tchttp.Response) error {
				body, err := json.Marshal(request)
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}

				payload := string(body)
				for _, want := range []string{
					`"CertificateType":"SVR"`,
					`"Alias":"example.com"`,
					`"Repeatable":true`,
				} {
					if !strings.Contains(payload, want) {
						t.Fatalf("request payload %q missing %s", payload, want)
					}
				}

				resp := response.(*tchttp.CommonResponse)
				if err := json.Unmarshal([]byte(`{"Response":{"CertificateId":"cert-123","RepeatCertId":"","RequestId":"req-1"}}`), resp); err != nil {
					t.Fatalf("unmarshal response: %v", err)
				}
				return nil
			},
		},
	}

	resp, err := uploader.Upload(UploadRequest{
		CertificatePEM: certPEM,
		PrivateKeyPEM:  keyPEM,
		Alias:          "example.com",
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if resp.CertificateID != "cert-123" || resp.RequestID != "req-1" {
		t.Fatalf("unexpected upload response: %+v", resp)
	}
}

func TestUploaderUploadRejectsInvalidPair(t *testing.T) {
	certPEM, _ := mustCreateTestKeyPair(t)

	uploader := &TencentUploader{Client: mockUploadClient{
		send: func(request tchttp.Request, response tchttp.Response) error {
			t.Fatal("client should not be called for invalid cert/key pair")
			return nil
		},
	}}

	if _, err := uploader.Upload(UploadRequest{
		CertificatePEM: certPEM,
		PrivateKeyPEM:  "not-a-key",
	}); err == nil {
		t.Fatal("Upload() expected error for invalid key")
	}
}

func mustCreateTestKeyPair(t *testing.T) (string, string) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "example.com",
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"example.com"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	if certPEM == nil || keyPEM == nil {
		t.Fatal("failed to encode PEM")
	}

	return string(certPEM), string(keyPEM)
}

func TestResolveCertificateMaterialRejectsMissingFiles(t *testing.T) {
	_, _, err := resolveCertificateMaterial(Config{
		CertPath: "/tmp/not-found.crt",
		KeyPath:  "/tmp/not-found.key",
	})
	if err == nil {
		t.Fatal("resolveCertificateMaterial() expected error for missing files")
	}
}

func TestValidatePEMPairRejectsMismatch(t *testing.T) {
	certPEM, _ := mustCreateTestKeyPair(t)
	_, otherKeyPEM := mustCreateTestKeyPair(t)

	err := validatePEMPair(certPEM, otherKeyPEM)
	if err == nil {
		t.Fatal("validatePEMPair() expected mismatch error")
	}
	if !strings.Contains(err.Error(), "invalid certificate/key pair") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func ExampleTencentUploader_Upload() {
	fmt.Println("uploaded certificate_id=cert-123 request_id=req-1")
	// Output: uploaded certificate_id=cert-123 request_id=req-1
}
