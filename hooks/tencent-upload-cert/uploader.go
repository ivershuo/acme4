package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tchttp "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/http"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

const sslAPIVersion = "2019-12-05"

type UploadRequest struct {
	CertificatePEM string
	PrivateKeyPEM  string
	Alias          string
}

type UploadResponse struct {
	CertificateID string
	RepeatCertID  string
	RequestID     string
}

type uploadCertificateAPI interface {
	Send(request tchttp.Request, response tchttp.Response) error
}

type TencentUploader struct {
	SecretID  string
	SecretKey string
	Client    uploadCertificateAPI
}

func (u *TencentUploader) Upload(req UploadRequest) (UploadResponse, error) {
	if err := validatePEMPair(req.CertificatePEM, req.PrivateKeyPEM); err != nil {
		return UploadResponse{}, err
	}

	client := u.Client
	if client == nil {
		client = newUploadClient(u.SecretID, u.SecretKey)
	}

	request := tchttp.NewCommonRequest("ssl", sslAPIVersion, "UploadCertificate")
	if err := request.SetActionParameters(map[string]interface{}{
		"CertificatePublicKey":  req.CertificatePEM,
		"CertificatePrivateKey": req.PrivateKeyPEM,
		"CertificateType":       "SVR",
		"Alias":                 req.Alias,
		"Repeatable":            true,
	}); err != nil {
		return UploadResponse{}, fmt.Errorf("build request: %w", err)
	}

	response := tchttp.NewCommonResponse()
	if err := client.Send(request, response); err != nil {
		return UploadResponse{}, err
	}

	var envelope struct {
		Response struct {
			CertificateID string `json:"CertificateId"`
			RepeatCertID  string `json:"RepeatCertId"`
			RequestID     string `json:"RequestId"`
		} `json:"Response"`
	}

	if err := json.Unmarshal(response.GetBody(), &envelope); err != nil {
		return UploadResponse{}, fmt.Errorf("decode response: %w", err)
	}

	if envelope.Response.CertificateID == "" {
		return UploadResponse{}, errors.New("empty certificate id in Tencent Cloud response")
	}

	return UploadResponse{
		CertificateID: envelope.Response.CertificateID,
		RepeatCertID:  envelope.Response.RepeatCertID,
		RequestID:     envelope.Response.RequestID,
	}, nil
}

func newUploadClient(secretID, secretKey string) uploadCertificateAPI {
	cred := tccommon.NewCredential(secretID, secretKey)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "ssl.tencentcloudapi.com"
	cpf.DisableRegionBreaker = true

	return tccommon.NewCommonClient(cred, "", cpf)
}

func resolveCertificateMaterial(cfg Config) (string, string, error) {
	certPath, keyPath := cfg.CertPath, cfg.KeyPath
	if cfg.Domain != "" {
		certPath = filepath.Join(cfg.CertDir, cfg.Domain+".crt")
		keyPath = filepath.Join(cfg.CertDir, cfg.Domain+".key")
	}

	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return "", "", fmt.Errorf("read cert file: %w", err)
	}

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return "", "", fmt.Errorf("read key file: %w", err)
	}

	certificatePEM := string(certBytes)
	privateKeyPEM := string(keyBytes)

	if err := validatePEMPair(certificatePEM, privateKeyPEM); err != nil {
		return "", "", err
	}

	return certificatePEM, privateKeyPEM, nil
}

func validatePEMPair(certificatePEM, privateKeyPEM string) error {
	if certificatePEM == "" || privateKeyPEM == "" {
		return errors.New("certificate and private key must not be empty")
	}

	if _, err := tls.X509KeyPair([]byte(certificatePEM), []byte(privateKeyPEM)); err != nil {
		return fmt.Errorf("invalid certificate/key pair: %w", err)
	}

	return nil
}
