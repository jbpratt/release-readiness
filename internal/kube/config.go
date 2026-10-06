package kube

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the connection settings resolved from one kubeconfig context.
// Only BearerToken is secret; never log or format it.
type Config struct {
	Context               string // resolved context name
	Server                string // API server URL
	CAData                []byte // PEM CA bundle; nil means system roots
	InsecureSkipTLSVerify bool
	BearerToken           string
	Namespace             string // context default namespace, may be empty
}

type kubeconfigYAML struct {
	CurrentContext string         `yaml:"current-context"`
	Clusters       []namedCluster `yaml:"clusters"`
	Contexts       []namedContext `yaml:"contexts"`
	Users          []namedUser    `yaml:"users"`
}

type namedCluster struct {
	Name    string      `yaml:"name"`
	Cluster clusterSpec `yaml:"cluster"`
}

type clusterSpec struct {
	Server                   string `yaml:"server"`
	CertificateAuthorityData string `yaml:"certificate-authority-data"`
	CertificateAuthority     string `yaml:"certificate-authority"`
	InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
}

type namedContext struct {
	Name    string      `yaml:"name"`
	Context contextSpec `yaml:"context"`
}

type contextSpec struct {
	Cluster   string `yaml:"cluster"`
	User      string `yaml:"user"`
	Namespace string `yaml:"namespace"`
}

type namedUser struct {
	Name string   `yaml:"name"`
	User userSpec `yaml:"user"`
}

type userSpec struct {
	Token                 string         `yaml:"token"`
	TokenFile             string         `yaml:"tokenFile"`
	ClientCertificate     string         `yaml:"client-certificate"`
	ClientCertificateData string         `yaml:"client-certificate-data"`
	Exec                  map[string]any `yaml:"exec"`
	AuthProvider          map[string]any `yaml:"auth-provider"`
}

// LoadKubeconfig reads the kubeconfig at path and resolves the named
// context (or the current-context when contextName is empty) into a
// Config. Only bearer-token and service-account tokenFile users are
// supported; exec, auth-provider, and client-certificate users return an
// error rather than being implemented.
func LoadKubeconfig(path, contextName string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig: %w", err)
	}

	var raw kubeconfigYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}

	name := contextName
	if name == "" {
		name = raw.CurrentContext
	}
	if name == "" {
		return nil, fmt.Errorf("no context specified and kubeconfig has no current-context")
	}

	ctx, ok := findContext(raw.Contexts, name)
	if !ok {
		return nil, fmt.Errorf("context %q not found in kubeconfig", name)
	}

	cluster, ok := findCluster(raw.Clusters, ctx.Cluster)
	if !ok {
		return nil, fmt.Errorf("cluster %q not found in kubeconfig", ctx.Cluster)
	}

	user, ok := findUser(raw.Users, ctx.User)
	if !ok {
		return nil, fmt.Errorf("user %q not found in kubeconfig", ctx.User)
	}

	dir := filepath.Dir(path)

	caData, err := resolveCAData(cluster, dir)
	if err != nil {
		return nil, err
	}

	token, err := resolveToken(user, dir)
	if err != nil {
		return nil, fmt.Errorf("user %q: %w", ctx.User, err)
	}

	return &Config{
		Context:               name,
		Server:                cluster.Server,
		CAData:                caData,
		InsecureSkipTLSVerify: cluster.InsecureSkipTLSVerify,
		BearerToken:           token,
		Namespace:             ctx.Namespace,
	}, nil
}

func findContext(contexts []namedContext, name string) (contextSpec, bool) {
	for _, c := range contexts {
		if c.Name == name {
			return c.Context, true
		}
	}
	return contextSpec{}, false
}

func findCluster(clusters []namedCluster, name string) (clusterSpec, bool) {
	for _, c := range clusters {
		if c.Name == name {
			return c.Cluster, true
		}
	}
	return clusterSpec{}, false
}

func findUser(users []namedUser, name string) (userSpec, bool) {
	for _, u := range users {
		if u.Name == name {
			return u.User, true
		}
	}
	return userSpec{}, false
}

func resolveCAData(cluster clusterSpec, baseDir string) ([]byte, error) {
	if cluster.CertificateAuthorityData != "" {
		decoded, err := base64.StdEncoding.DecodeString(cluster.CertificateAuthorityData)
		if err != nil {
			return nil, fmt.Errorf("decode certificate-authority-data: %w", err)
		}
		return decoded, nil
	}
	if cluster.CertificateAuthority != "" {
		data, err := os.ReadFile(resolvePath(cluster.CertificateAuthority, baseDir))
		if err != nil {
			return nil, fmt.Errorf("read certificate-authority: %w", err)
		}
		return data, nil
	}
	return nil, nil
}

func resolveToken(user userSpec, baseDir string) (string, error) {
	if user.Token != "" {
		return user.Token, nil
	}
	if user.TokenFile != "" {
		data, err := os.ReadFile(resolvePath(user.TokenFile, baseDir))
		if err != nil {
			return "", fmt.Errorf("read tokenFile: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	if len(user.Exec) > 0 || len(user.AuthProvider) > 0 || user.ClientCertificate != "" || user.ClientCertificateData != "" {
		return "", fmt.Errorf("unsupported auth: only bearer token or tokenFile users are supported (not exec, auth-provider, or client-certificate)")
	}
	return "", fmt.Errorf("unsupported auth: no token or tokenFile found")
}

func resolvePath(p, baseDir string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(baseDir, p)
}
