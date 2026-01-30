use crate::aws::{SecretsManagerClient, SsmParameterStore};
use std::env;
use tracing::info;

#[derive(Debug)]
pub enum ConfigError {
    Missing(String),
    Aws(String),
}

impl std::fmt::Display for ConfigError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ConfigError::Missing(key) => write!(f, "Config key '{}' not found", key),
            ConfigError::Aws(msg) => write!(f, "AWS error: {}", msg),
        }
    }
}

impl std::error::Error for ConfigError {}

#[derive(Clone, Debug)]
pub struct Config {
    pub host: String,
    pub port: u16,
    pub kafka_brokers: String,
    pub kafka_topic: String,
    pub kafka_sasl_username: Option<String>,
    pub kafka_sasl_password: Option<String>,
    pub schema_registry_url: String,
    pub schema_registry_key: Option<String>,
    pub schema_registry_secret: Option<String>,
}

impl Config {
    /// Load configuration with env-first approach and optional AWS fallback.
    /// Priority: Environment variables -> AWS SSM/Secrets Manager (if AWS_CONFIG_ENABLED=true)
    pub async fn load() -> Result<Self, Box<dyn std::error::Error + Send + Sync>> {
        let env_name = env::var("APP_ENV").unwrap_or_else(|_| "dev".to_string());
        let aws_enabled = env::var("AWS_CONFIG_ENABLED")
            .map(|v| v == "true")
            .unwrap_or(false);

        info!(
            "Loading configuration (env: {}, aws_enabled: {})",
            env_name, aws_enabled
        );

        // Initialize AWS clients if enabled
        let (params, secrets) = if aws_enabled {
            let aws_config = aws_config::load_defaults(aws_config::BehaviorVersion::latest()).await;
            let params = Some(SsmParameterStore::new(&aws_config));
            let secrets = Some(SecretsManagerClient::new(&aws_config));
            info!("AWS config loading enabled");
            (params, secrets)
        } else {
            info!("AWS config loading disabled, using environment variables only");
            (None, None)
        };

        // Host and port with defaults
        let host = env::var("TRACKING_HOST").unwrap_or_else(|_| "0.0.0.0".to_string());
        let port: u16 = env::var("TRACKING_PORT")
            .ok()
            .and_then(|p| p.parse().ok())
            .unwrap_or(3000);

        // Kafka brokers (required)
        let kafka_brokers = Self::get_required(
            "KAFKA_BOOTSTRAP_SERVERS",
            "kafka/bootstrap_servers",
            &params,
        )
        .await?;
        info!("Loaded kafka brokers");

        // Kafka topic (optional with default)
        let kafka_topic = Self::get_optional(
            "KAFKA_TRACKING_TOPIC",
            "kafka/tracking/topic",
            "tracking-events",
            &params,
        )
        .await;
        info!("Kafka topic: {}", kafka_topic);

        // Schema Registry URL (required)
        let schema_registry_url = Self::get_required(
            "SCHEMA_REGISTRY_URL",
            "kafka/schema_registry/endpoint",
            &params,
        )
        .await?;
        info!("Loaded schema registry URL");

        // Optional SASL credentials
        let kafka_sasl_username =
            Self::get_secret_optional("KAFKA_SASL_USERNAME", "kafka/sasl/username", &secrets).await;
        let kafka_sasl_password =
            Self::get_secret_optional("KAFKA_SASL_PASSWORD", "kafka/sasl/password", &secrets).await;

        if kafka_sasl_username.is_some() {
            info!("SASL authentication enabled");
        }

        // Optional Schema Registry credentials
        let schema_registry_key =
            Self::get_secret_optional("SCHEMA_REGISTRY_KEY", "kafka/schema_registry/key", &secrets)
                .await;
        let schema_registry_secret = Self::get_secret_optional(
            "SCHEMA_REGISTRY_SECRET",
            "kafka/schema_registry/secret",
            &secrets,
        )
        .await;

        if schema_registry_key.is_some() {
            info!("Schema Registry authentication enabled");
        }

        Ok(Self {
            host,
            port,
            kafka_brokers,
            kafka_topic,
            kafka_sasl_username,
            kafka_sasl_password,
            schema_registry_url,
            schema_registry_key,
            schema_registry_secret,
        })
    }

    /// Load configuration from AWS only (legacy method for backwards compatibility)
    pub async fn from_aws(env: &str) -> Result<Self, Box<dyn std::error::Error + Send + Sync>> {
        info!("Loading configuration from AWS for environment: {}", env);

        let aws_config = aws_config::load_defaults(aws_config::BehaviorVersion::latest()).await;
        let params = SsmParameterStore::new(&aws_config);
        let secrets = SecretsManagerClient::new(&aws_config);

        // Load from Parameter Store
        let kafka_brokers = params.get("kafka/bootstrap_servers").await?;
        info!("Loaded kafka/bootstrap_servers");

        let schema_registry_url = params.get("kafka/schema_registry/endpoint").await?;
        info!("Loaded kafka/schema_registry/endpoint");

        let kafka_topic = params
            .get_optional("kafka/tracking/topic")
            .await
            .unwrap_or_else(|| "tracking-events".to_string());
        info!("Kafka topic: {}", kafka_topic);

        let host = params
            .get_optional(&format!("/warmbly/{}/tracking/host", env))
            .await
            .unwrap_or_else(|| "0.0.0.0".to_string());

        let port: u16 = params
            .get_optional(&format!("/warmbly/{}/tracking/port", env))
            .await
            .unwrap_or_else(|| "3000".to_string())
            .parse()?;

        // Load from Secrets Manager
        let kafka_sasl_username = secrets.get_optional("kafka/sasl/username").await;
        let kafka_sasl_password = secrets.get_optional("kafka/sasl/password").await;
        let schema_registry_key = secrets.get_optional("kafka/schema_registry/key").await;
        let schema_registry_secret = secrets.get_optional("kafka/schema_registry/secret").await;

        if kafka_sasl_username.is_some() {
            info!("SASL authentication enabled");
        }
        if schema_registry_key.is_some() {
            info!("Schema Registry authentication enabled");
        }

        Ok(Self {
            host,
            port,
            kafka_brokers,
            kafka_topic,
            kafka_sasl_username,
            kafka_sasl_password,
            schema_registry_url,
            schema_registry_key,
            schema_registry_secret,
        })
    }

    /// Get required config value - env first, then AWS SSM if enabled
    async fn get_required(
        env_key: &str,
        aws_key: &str,
        params: &Option<SsmParameterStore>,
    ) -> Result<String, ConfigError> {
        // Check env var first
        if let Ok(val) = env::var(env_key) {
            if !val.is_empty() {
                return Ok(val);
            }
        }

        // Fall back to AWS if enabled
        if let Some(params) = params {
            match params.get(aws_key).await {
                Ok(val) => return Ok(val),
                Err(e) => {
                    return Err(ConfigError::Aws(format!(
                        "Failed to get {} from AWS: {}",
                        aws_key, e
                    )))
                }
            }
        }

        Err(ConfigError::Missing(env_key.to_string()))
    }

    /// Get optional config value with default - env first, then AWS SSM if enabled
    async fn get_optional(
        env_key: &str,
        aws_key: &str,
        default: &str,
        params: &Option<SsmParameterStore>,
    ) -> String {
        // Check env var first
        if let Ok(val) = env::var(env_key) {
            if !val.is_empty() {
                return val;
            }
        }

        // Fall back to AWS if enabled
        if let Some(params) = params {
            if let Some(val) = params.get_optional(aws_key).await {
                return val;
            }
        }

        default.to_string()
    }

    /// Get optional secret value - env first, then AWS Secrets Manager if enabled
    async fn get_secret_optional(
        env_key: &str,
        aws_key: &str,
        secrets: &Option<SecretsManagerClient>,
    ) -> Option<String> {
        // Check env var first
        if let Ok(val) = env::var(env_key) {
            if !val.is_empty() {
                return Some(val);
            }
        }

        // Fall back to AWS if enabled
        if let Some(secrets) = secrets {
            return secrets.get_optional(aws_key).await;
        }

        None
    }

    pub fn addr(&self) -> String {
        format!("{}:{}", self.host, self.port)
    }

    pub fn sasl_enabled(&self) -> bool {
        self.kafka_sasl_username.is_some() && self.kafka_sasl_password.is_some()
    }

    pub fn schema_registry_auth(&self) -> Option<(String, String)> {
        match (&self.schema_registry_key, &self.schema_registry_secret) {
            (Some(key), Some(secret)) => Some((key.clone(), secret.clone())),
            _ => None,
        }
    }
}
