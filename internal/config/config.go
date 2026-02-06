// Package config provides configuration management for the CWL service.
package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for the CWL service.
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	MongoDB  MongoDBConfig  `mapstructure:"mongodb"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Auth     AuthConfig     `mapstructure:"auth"`
	BVBRC    BVBRCConfig    `mapstructure:"bvbrc"`
	Storage  StorageConfig  `mapstructure:"storage"`
	Executor ExecutorConfig `mapstructure:"executor"`
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// MongoDBConfig holds MongoDB connection configuration.
type MongoDBConfig struct {
	URI      string `mapstructure:"uri"`
	Database string `mapstructure:"database"`
}

// RedisConfig holds Redis connection configuration.
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	ServiceToken       string   `mapstructure:"service_token"`
	ValidateUserTokens bool     `mapstructure:"validate_user_tokens"`
	WorkspaceURL       string   `mapstructure:"workspace_url"`
	UserServiceURL     string   `mapstructure:"user_service_url"`
	AdminUsers         []string `mapstructure:"admin_users"`
}

// BVBRCConfig holds BV-BRC integration configuration.
type BVBRCConfig struct {
	AppServiceURL     string        `mapstructure:"app_service_url"`
	AppServiceTimeout time.Duration `mapstructure:"app_service_timeout"`
	DatabaseDSN       string        `mapstructure:"database_dsn"`
	CWLStepRunnerID   string        `mapstructure:"cwl_step_runner_id"`
}

// StorageConfig holds file storage configuration.
type StorageConfig struct {
	LocalPath    string `mapstructure:"local_path"`
	WorkspaceURL string `mapstructure:"workspace_url"`
	ShockURL     string `mapstructure:"shock_url"`
}

// ExecutorConfig holds executor configuration.
type ExecutorConfig struct {
	Mode           string          `mapstructure:"mode"` // "bvbrc" or "local"
	MaxRetries     int             `mapstructure:"max_retries"`
	RetryDelay     time.Duration   `mapstructure:"retry_delay"`
	PollInterval   time.Duration   `mapstructure:"poll_interval"`
	DefaultCPU     int             `mapstructure:"default_cpu"`
	DefaultMemory  int             `mapstructure:"default_memory"`  // MB
	DefaultRuntime int             `mapstructure:"default_runtime"` // seconds
	Container      ContainerConfig `mapstructure:"container"`
}

// ContainerConfig holds container runtime configuration.
type ContainerConfig struct {
	Runtime       string `mapstructure:"runtime"`        // "docker", "podman", "apptainer"
	ApptainerPath string `mapstructure:"apptainer_path"` // Path to apptainer/singularity binary
	DockerPath    string `mapstructure:"docker_path"`    // Path to docker binary
	PodmanPath    string `mapstructure:"podman_path"`    // Path to podman binary
	CacheDir      string `mapstructure:"cache_dir"`      // Container image cache directory
	PullPolicy    string `mapstructure:"pull_policy"`    // "always", "if-not-present", "never"
	GPUEnabled    bool   `mapstructure:"gpu_enabled"`    // Enable GPU passthrough
	GPURuntime    string `mapstructure:"gpu_runtime"`    // "nvidia", "amd"
}

// Load reads configuration from file and environment variables.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.read_timeout", 30*time.Second)
	v.SetDefault("server.write_timeout", 30*time.Second)

	v.SetDefault("mongodb.uri", "mongodb://localhost:27017")
	v.SetDefault("mongodb.database", "cwe_cwl")

	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)

	v.SetDefault("auth.validate_user_tokens", true)
	v.SetDefault("auth.workspace_url", "https://p3.theseed.org/services/Workspace")
	v.SetDefault("auth.user_service_url", "https://user.patricbrc.org")
	v.SetDefault("auth.admin_users", []string{})

	v.SetDefault("bvbrc.app_service_url", "https://p3.theseed.org/services/app_service")
	v.SetDefault("bvbrc.app_service_timeout", 30*time.Second)
	v.SetDefault("bvbrc.cwl_step_runner_id", "CWLStepRunner")

	v.SetDefault("storage.local_path", "/data/cwe-cwl/uploads")

	v.SetDefault("executor.mode", "bvbrc")
	v.SetDefault("executor.max_retries", 3)
	v.SetDefault("executor.retry_delay", 30*time.Second)
	v.SetDefault("executor.poll_interval", 5*time.Second)
	v.SetDefault("executor.default_cpu", 1)
	v.SetDefault("executor.default_memory", 4096)
	v.SetDefault("executor.default_runtime", 86400)

	// Container runtime defaults
	v.SetDefault("executor.container.runtime", "apptainer")
	v.SetDefault("executor.container.apptainer_path", "apptainer")
	v.SetDefault("executor.container.docker_path", "docker")
	v.SetDefault("executor.container.podman_path", "podman")
	v.SetDefault("executor.container.cache_dir", "/data/cwe-cwl/containers")
	v.SetDefault("executor.container.pull_policy", "if-not-present")
	v.SetDefault("executor.container.gpu_enabled", true)
	v.SetDefault("executor.container.gpu_runtime", "nvidia")

	// Read config file if specified
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./configs")
		v.AddConfigPath("/etc/cwe-cwl")
	}

	// Read environment variables
	v.SetEnvPrefix("CWE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Try to read config file (ignore if not found)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
