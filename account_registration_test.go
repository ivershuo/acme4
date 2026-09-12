package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-acme/lego/v4/acme"
	"github.com/go-acme/lego/v4/registration"
)

func TestRegistrationIsPersistedPerCA(t *testing.T) {
	dir := t.TempDir()
	user := &MyUser{accountDir: dir, Registration: &registration.Resource{URI: "https://ca-one.test/acct/1", Body: acme.Account{Status: acme.StatusValid}}}
	if err := saveRegistrationForCA(user, "https://ca-one.test/directory"); err != nil {
		t.Fatal(err)
	}
	loaded := &MyUser{accountDir: dir}
	if err := loadRegistrationForCA(loaded, "https://ca-one.test/directory"); err != nil {
		t.Fatal(err)
	}
	if loaded.Registration == nil || loaded.Registration.URI != user.Registration.URI {
		t.Fatalf("registration not restored: %+v", loaded.Registration)
	}
	if err := loadRegistrationForCA(loaded, "https://ca-two.test/directory"); err != nil {
		t.Fatal(err)
	}
	if loaded.Registration != nil {
		t.Fatal("registration leaked across CA directories")
	}
}

func TestDamagedAccountKeyIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin@example.com.key")
	original := []byte("damaged account key")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateUser("admin@example.com", dir); err == nil {
		t.Fatal("loadOrCreateUser() expected damaged PEM error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatal("damaged account key was overwritten")
	}
}
