package controller

import (
	"crypto/sha256"
	"fmt"

	keylineconfig "github.com/The127/Keyline/config"
	keylinev1alpha1 "github.com/keyline/keyline-operator/api/v1alpha1"
	"gopkg.in/yaml.v3"
)

const (
	operatorUsername      = "keyline-operator"
	operatorApplication   = "keyline-operator"
	keyDirPath            = "/keys"
	configFilePath        = "/etc/keyline/config.yaml"
	keylinePort           = 8080
	defaultVirtualServer  = "keyline"
	defaultDatabaseName   = "keyline"
	keylineAppName        = "keyline"
	keyStoreModeDirectory = "directory"
)

func buildKeylineConfig(
	instance *keylinev1alpha1.KeylineInstance,
	pubKeyPEM, kid, dbUser, dbPass, vaultToken string,
) (data []byte, hash string, err error) {
	vs := instance.Spec.VirtualServer
	if vs == "" {
		vs = defaultVirtualServer
	}

	var dbCfg keylineconfig.DatabaseConfig
	switch instance.Spec.Database.Mode {
	case "memory":
		dbCfg = keylineconfig.DatabaseConfig{Mode: keylineconfig.DatabaseModeMemory}
	default:
		pg := instance.Spec.Database.Postgres
		pgPort := int(pg.Port)
		if pgPort == 0 {
			pgPort = 5432
		}
		dbName := pg.Database
		if dbName == "" {
			dbName = defaultDatabaseName
		}
		sslMode := pg.SslMode
		if sslMode == "" {
			sslMode = "enable"
		}
		dbCfg = keylineconfig.DatabaseConfig{
			Mode: keylineconfig.DatabaseModePostgres,
			Postgres: keylineconfig.PostgresConfig{
				Host:     pg.Host,
				Port:     pgPort,
				Database: dbName,
				Username: dbUser,
				Password: dbPass,
				SslMode:  sslMode,
			},
		}
	}

	cfg := keylineconfig.Config{
		Server: keylineconfig.ServerConfig{
			Host:           "0.0.0.0",
			Port:           keylinePort,
			ExternalUrl:    instance.Spec.ExternalUrl,
			AllowedOrigins: []string{instance.Spec.ExternalUrl, instance.Spec.FrontendExternalUrl},
		},
		Frontend: struct {
			ExternalUrl string `yaml:"externalUrl"`
		}{
			ExternalUrl: instance.Spec.FrontendExternalUrl,
		},
		Database: dbCfg,
		InitialVirtualServer: keylineconfig.InitialVirtualServerConfig{
			Name:                  vs,
			EnableRegistration:    false,
			SigningAlgorithm:      keylineconfig.SigningAlgorithmEdDSA,
			CreateSystemAdminRole: true,
			CreateAdmin:           false,
			ServiceUsers: []keylineconfig.ServiceUserConfig{
				{
					Username: operatorUsername,
					Roles:    []string{"system:admin", "system:system-admin"},
					PublicKey: struct {
						Pem string `yaml:"pem"`
						Kid string `yaml:"kid"`
					}{
						Pem: pubKeyPEM,
						Kid: kid,
					},
				},
			},
			Projects: []keylineconfig.InitialProjectConfig{
				{
					Slug: "operator",
					Name: "Operator",
					Applications: []struct {
						Name                   string   `yaml:"name"`
						DisplayName            string   `yaml:"displayName"`
						Type                   string   `yaml:"type"`
						HashedSecret           *string  `yaml:"hashedSecret,omitempty"`
						RedirectUris           []string `yaml:"redirectUris"`
						PostLogoutRedirectUris []string `yaml:"postLogoutRedirectUris"`
						DeviceFlowEnabled      bool     `yaml:"deviceFlowEnabled"`
					}{
						{
							Name:        operatorApplication,
							DisplayName: "Keyline Operator",
							Type:        "public",
						},
					},
				},
			},
		},
		Cache: struct {
			Mode  keylineconfig.CacheMode `yaml:"mode"`
			Redis struct {
				Host     string `yaml:"host"`
				Port     int    `yaml:"port"`
				Username string `yaml:"username"`
				Password string `yaml:"password"`
				Database int    `yaml:"database"`
			} `yaml:"redis"`
		}{
			Mode: keylineconfig.CacheModeMemory,
		},
		LeaderElection: keylineconfig.LeaderElectionConfig{
			Mode: keylineconfig.LeaderElectionModeNone,
		},
	}

	switch instance.Spec.KeyStore.Mode {
	case keyStoreModeDirectory:
		cfg.KeyStore = keylineconfig.KeyStoreConfig{
			Mode: keylineconfig.KeyStoreModeDirectory,
			Directory: struct {
				Path string `yaml:"path"`
			}{Path: keyDirPath},
		}
	case "vault":
		v := instance.Spec.KeyStore.Vault
		cfg.KeyStore = keylineconfig.KeyStoreConfig{
			Mode: keylineconfig.KeyStoreModeVault,
			Vault: keylineconfig.VaultKeyStoreConfig{
				Address: v.Address,
				Token:   vaultToken,
				Mount:   v.Mount,
				Prefix:  v.Prefix,
			},
		}
	}

	data, err = yaml.Marshal(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling keyline config: %w", err)
	}

	sum := sha256.Sum256(data)
	hash = fmt.Sprintf("%x", sum)
	return data, hash, nil
}
