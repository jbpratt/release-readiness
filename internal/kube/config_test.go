package kube

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeKubeconfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadKubeconfig_ContextPin(t *testing.T) {
	const yaml = `
apiVersion: v1
current-context: ctx-a
clusters:
- name: cluster-a
  cluster:
    server: https://a.example.com:6443
- name: cluster-b
  cluster:
    server: https://b.example.com:6443
contexts:
- name: ctx-a
  context:
    cluster: cluster-a
    user: user-a
    namespace: ns-a
- name: ctx-b
  context:
    cluster: cluster-b
    user: user-b
    namespace: ns-b
users:
- name: user-a
  user:
    token: fake-token-a
- name: user-b
  user:
    token: fake-token-b
`
	path := writeKubeconfig(t, yaml)

	kc, err := LoadKubeconfig(path, "ctx-b")
	if err != nil {
		t.Fatal(err)
	}
	if kc.Context != "ctx-b" {
		t.Errorf("Context = %q, want ctx-b", kc.Context)
	}
	if kc.Server != "https://b.example.com:6443" {
		t.Errorf("Server = %q, want https://b.example.com:6443", kc.Server)
	}
	if kc.Namespace != "ns-b" {
		t.Errorf("Namespace = %q, want ns-b", kc.Namespace)
	}
	if kc.BearerToken != "fake-token-b" {
		t.Errorf("BearerToken = %q, want fake-token-b", kc.BearerToken)
	}

	// Empty contextName falls back to current-context.
	kc, err = LoadKubeconfig(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if kc.Context != "ctx-a" {
		t.Errorf("Context = %q, want ctx-a", kc.Context)
	}
}

func TestLoadKubeconfig_CAData(t *testing.T) {
	caData := "FAKE-CERT-DATA"
	encoded := base64.StdEncoding.EncodeToString([]byte(caData))

	yaml := `
apiVersion: v1
current-context: ctx
clusters:
- name: c
  cluster:
    server: https://example.com:6443
    certificate-authority-data: ` + encoded + `
contexts:
- name: ctx
  context:
    cluster: c
    user: u
    namespace: ns
users:
- name: u
  user:
    token: fake-token
`
	path := writeKubeconfig(t, yaml)

	kc, err := LoadKubeconfig(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(kc.CAData) != caData {
		t.Errorf("CAData = %q, want %q", kc.CAData, caData)
	}
}

func TestLoadKubeconfig_TokenFile(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("fake-token-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	yaml := `
apiVersion: v1
current-context: ctx
clusters:
- name: c
  cluster:
    server: https://example.com:6443
contexts:
- name: ctx
  context:
    cluster: c
    user: u
    namespace: ns
users:
- name: u
  user:
    tokenFile: ` + tokenPath + `
`
	path := writeKubeconfig(t, yaml)

	kc, err := LoadKubeconfig(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if kc.BearerToken != "fake-token-from-file" {
		t.Errorf("BearerToken = %q, want fake-token-from-file", kc.BearerToken)
	}
}

func TestLoadKubeconfig_RelativeTokenFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("fake-token-from-relative-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	const yaml = `
apiVersion: v1
current-context: ctx
clusters:
- name: c
  cluster:
    server: https://example.com:6443
contexts:
- name: ctx
  context:
    cluster: c
    user: u
    namespace: ns
users:
- name: u
  user:
    tokenFile: token
`
	path := filepath.Join(dir, "kubeconfig.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	kc, err := LoadKubeconfig(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if kc.BearerToken != "fake-token-from-relative-file" {
		t.Errorf("BearerToken = %q, want fake-token-from-relative-file", kc.BearerToken)
	}
}

func TestLoadKubeconfig_UnsupportedExec(t *testing.T) {
	const yaml = `
apiVersion: v1
current-context: ctx
clusters:
- name: c
  cluster:
    server: https://example.com:6443
contexts:
- name: ctx
  context:
    cluster: c
    user: u
    namespace: ns
users:
- name: u
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: fake-exec-cmd
`
	path := writeKubeconfig(t, yaml)

	_, err := LoadKubeconfig(path, "")
	if err == nil {
		t.Fatal("expected error for exec-based user, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported auth") {
		t.Errorf("error = %q, want it to mention unsupported auth", err)
	}
}
