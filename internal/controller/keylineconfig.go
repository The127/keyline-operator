package controller

import (
	"crypto/sha256"
	"fmt"

	keylinev1alpha1 "github.com/keyline/keyline-operator/api/v1alpha1"
	"gopkg.in/yaml.v3"
)

const (
	operatorUsername    = "keyline-operator"
	operatorApplication = "keyline-operator"
	keyDirPath          = "/keys"
	configFilePath      = "/etc/keyline/config.yaml"
	keylinePort         = 8080
)

type keylineConfigYAML struct {
	Server               keylineServerCfg               `yaml:"server"`
	Frontend             keylineFrontendCfg             `yaml:"frontend"`
	Database             keylineDatabaseCfg             `yaml:"database"`
	InitialVirtualServer keylineInitialVirtualServerCfg `yaml:"initialVirtualServer"`
	KeyStore             keylineKeyStoreCfg             `yaml:"keyStore"`
	Cache                keylineCacheCfg                `yaml:"cache"`
	LeaderElection       keylineLeaderElectionCfg       `yaml:"leaderElection"`
}

type keylineServerCfg struct {
	Host           string   `yaml:"host"`
	Port           int      `yaml:"port"`
	ExternalURL    string   `yaml:"externalUrl"`
	AllowedOrigins []string `yaml:"allowedOrigins"`
}

type keylineFrontendCfg struct {
	ExternalURL string `yaml:"externalUrl"`
}

type keylineDatabaseCfg struct {
	Mode     string             `yaml:"mode"`
	Postgres keylinePostgresCfg `yaml:"postgres"`
}

type keylinePostgresCfg struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	SslMode  string `yaml:"sslMode"`
}

type keylineInitialVirtualServerCfg struct {
	Name                  string                  `yaml:"name"`
	EnableRegistration    bool                    `yaml:"enableRegistration"`
	SigningAlgorithm      string                  `yaml:"signingAlgorithm"`
	CreateSystemAdminRole bool                    `yaml:"createSystemAdminRole"`
	CreateAdmin           bool                    `yaml:"createAdmin"`
	ServiceUsers          []keylineServiceUserCfg `yaml:"serviceUsers"`
	Projects              []keylineInitialProject `yaml:"projects"`
}

type keylineServiceUserCfg struct {
	Username  string              `yaml:"username"`
	Roles     []string            `yaml:"roles"`
	PublicKey keylinePublicKeyCfg `yaml:"publicKey"`
}

type keylinePublicKeyCfg struct {
	Pem string `yaml:"pem"`
	Kid string `yaml:"kid"`
}

type keylineInitialProject struct {
	Slug         string                 `yaml:"slug"`
	Name         string                 `yaml:"name"`
	Applications []keylineInitialAppCfg `yaml:"applications"`
}

type keylineInitialAppCfg struct {
	Name        string `yaml:"name"`
	DisplayName string `yaml:"displayName"`
	Type        string `yaml:"type"`
}

type keylineKeyStoreCfg struct {
	Mode      string                   `yaml:"mode"`
	Directory *keylineKeyStoreDirCfg   `yaml:"directory,omitempty"`
	Vault     *keylineKeyStoreVaultCfg `yaml:"vault,omitempty"`
}

type keylineKeyStoreDirCfg struct {
	Path string `yaml:"path"`
}

type keylineKeyStoreVaultCfg struct {
	Address string `yaml:"address"`
	Token   string `yaml:"token"`
	Mount   string `yaml:"mount"`
	Prefix  string `yaml:"prefix,omitempty"`
}

type keylineCacheCfg struct {
	Mode string `yaml:"mode"`
}

type keylineLeaderElectionCfg struct {
	Mode string `yaml:"mode"`
}

func buildKeylineConfig(
	instance *keylinev1alpha1.KeylineInstance,
	pubKeyPEM, kid, dbUser, dbPass, vaultToken string,
) (data []byte, hash string, err error) {
	vs := instance.Spec.VirtualServer
	if vs == "" {
		vs = "keyline"
	}

	var dbCfg keylineDatabaseCfg
	switch instance.Spec.Database.Mode {
	case "memory":
		dbCfg = keylineDatabaseCfg{Mode: "memory"}
	default:
		pg := instance.Spec.Database.Postgres
		pgPort := int(pg.Port)
		if pgPort == 0 {
			pgPort = 5432
		}
		dbName := pg.Database
		if dbName == "" {
			dbName = "keyline"
		}
		sslMode := pg.SslMode
		if sslMode == "" {
			sslMode = "enable"
		}
		dbCfg = keylineDatabaseCfg{
			Mode: "postgres",
			Postgres: keylinePostgresCfg{
				Host:     pg.Host,
				Port:     pgPort,
				Database: dbName,
				Username: dbUser,
				Password: dbPass,
				SslMode:  sslMode,
			},
		}
	}

	cfg := keylineConfigYAML{
		Server: keylineServerCfg{
			Host:           "0.0.0.0",
			Port:           keylinePort,
			ExternalURL:    instance.Spec.ExternalUrl,
			AllowedOrigins: []string{instance.Spec.ExternalUrl, instance.Spec.FrontendExternalUrl},
		},
		Frontend: keylineFrontendCfg{
			ExternalURL: instance.Spec.FrontendExternalUrl,
		},
		Database: dbCfg,
		InitialVirtualServer: keylineInitialVirtualServerCfg{
			Name:                  vs,
			EnableRegistration:    false,
			SigningAlgorithm:      "EdDSA",
			CreateSystemAdminRole: true,
			CreateAdmin:           false,
			ServiceUsers: []keylineServiceUserCfg{
				{
					Username: operatorUsername,
					Roles:    []string{"system:admin", "system:system-admin"},
					PublicKey: keylinePublicKeyCfg{
						Pem: pubKeyPEM,
						Kid: kid,
					},
				},
			},
			Projects: []keylineInitialProject{
				{
					Slug: "operator",
					Name: "Operator",
					Applications: []keylineInitialAppCfg{
						{
							Name:        operatorApplication,
							DisplayName: "Keyline Operator",
							Type:        "public",
						},
					},
				},
			},
		},
		Cache:          keylineCacheCfg{Mode: "memory"},
		LeaderElection: keylineLeaderElectionCfg{Mode: "none"},
	}

	switch instance.Spec.KeyStore.Mode {
	case "directory":
		cfg.KeyStore = keylineKeyStoreCfg{
			Mode:      "directory",
			Directory: &keylineKeyStoreDirCfg{Path: keyDirPath},
		}
	case "vault":
		v := instance.Spec.KeyStore.Vault
		cfg.KeyStore = keylineKeyStoreCfg{
			Mode: "vault",
			Vault: &keylineKeyStoreVaultCfg{
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
