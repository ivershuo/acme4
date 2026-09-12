package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-acme/lego/v4/registration"
)

func registrationPath(accountDir, caURL string) string {
	sum := sha256.Sum256([]byte(caURL))
	return filepath.Join(accountDir, "registrations", hex.EncodeToString(sum[:12])+".json")
}

func loadRegistrationForCA(user *MyUser, caURL string) error {
	if user.registrationCA == caURL {
		return nil
	}
	user.Registration = nil
	user.registrationCA = caURL
	data, err := os.ReadFile(registrationPath(user.accountDir, caURL))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var resource registration.Resource
	if err := json.Unmarshal(data, &resource); err != nil {
		return fmt.Errorf("decode registration: %w", err)
	}
	if resource.URI == "" {
		return fmt.Errorf("registration URI is empty")
	}
	user.Registration = &resource
	return nil
}

func saveRegistrationForCA(user *MyUser, caURL string) error {
	if user.Registration == nil {
		return fmt.Errorf("registration is nil")
	}
	dir := filepath.Dir(registrationPath(user.accountDir, caURL))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(user.Registration, "", "  ")
	if err != nil {
		return err
	}
	temp, err := writeTempFile(dir, ".registration-*", append(data, '\n'))
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	return replaceFile(temp, registrationPath(user.accountDir, caURL))
}
